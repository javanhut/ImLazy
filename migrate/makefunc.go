package migrate

import (
	"sort"
	"strconv"
	"strings"
)

// GNU make expands text functions itself; the shell never sees them. ImLazy
// hands command strings to bash, where "$(strip x)" is command substitution
// and "$(if a,b,)" is a syntax error, so the migrator has to resolve these at
// migrate time instead of copying them through.

// makeFuncArity lists the make text functions we can evaluate, mapped to the
// number of arguments each takes (-1 = variadic). Fixed-arity functions split
// their argument list at most arity-1 times, so a trailing argument may itself
// contain commas — same as make.
var makeFuncArity = map[string]int{
	"strip": 1, "subst": 3, "patsubst": 3, "filter": 2, "filter-out": 2,
	"findstring": 2, "sort": 1, "word": 2, "wordlist": 3, "words": 1,
	"firstword": 1, "lastword": 1, "dir": 1, "notdir": 1, "suffix": 1,
	"basename": 1, "addsuffix": 2, "addprefix": 2, "join": 2,
	"and": -1, "or": -1,
}

// makeFuncUnsupported lists make functions with no static or shell equivalent.
// Recipes using them are turned into a failing stub by the converter so the
// breakage shows up at migrate time rather than as a bash syntax error.
var makeFuncUnsupported = map[string]bool{
	"foreach": true, "call": true, "eval": true, "value": true,
	"origin": true, "flavor": true, "error": true, "warning": true,
	"info": true, "guile": true, "file": true,
}

const (
	maxExpandDepth = 32
	protectMark    = "\x00"
)

// makeEval expands make variable references and text functions.
type makeEval struct {
	vars        map[string]string // make variable name → raw value
	expanding   map[string]bool   // recursion guard for variable expansion
	usedVars    map[string]bool   // variables frozen into an evaluated result
	notes       []string          // conversions worth telling the user about
	protected   []string          // text hidden from later regex passes
	unsupported []string          // make expressions we could not convert
	evaluated   bool              // at least one function was evaluated
}

func newMakeEval(ir *MakefileIR) *makeEval {
	vars := make(map[string]string)
	if ir != nil {
		for _, v := range ir.Variables {
			vars[v.Name] = v.Value
		}
	}
	return &makeEval{
		vars:      vars,
		expanding: make(map[string]bool),
		usedVars:  make(map[string]bool),
	}
}

// protect hides text behind a placeholder so the $(VAR) → {{var}} pass cannot
// rewrite shell substitutions or expressions we deliberately left alone.
func (e *makeEval) protect(s string) string {
	e.protected = append(e.protected, s)
	return protectMark + "p" + strconv.Itoa(len(e.protected)-1) + protectMark
}

// restore puts protected text back.
func (e *makeEval) restore(s string) string {
	for i, p := range e.protected {
		s = strings.ReplaceAll(s, protectMark+"p"+strconv.Itoa(i)+protectMark, p)
	}
	return s
}

// frozenVars lists the variables whose values were baked into an evaluated
// result, as "NAME=value" pairs.
func (e *makeEval) frozenVars() []string {
	names := make([]string, 0, len(e.usedVars))
	for name := range e.usedVars {
		names = append(names, name)
	}
	sort.Strings(names)
	pairs := make([]string, 0, len(names))
	for _, name := range names {
		pairs = append(pairs, name+"="+strconv.Quote(e.vars[name]))
	}
	return pairs
}

// expand rewrites make expressions in s. With keepVars set, plain $(VAR)
// references are left for the caller to turn into {{var}}; inside function
// arguments they are expanded from the variable table, since evaluating a
// function needs literal text. The second return value reports whether the
// result is fully known at migrate time.
func (e *makeEval) expand(s string, keepVars bool, depth int) (string, bool) {
	if depth > maxExpandDepth {
		return s, false
	}

	var b strings.Builder
	static := true

	for i := 0; i < len(s); {
		if s[i] != '$' || i+1 >= len(s) {
			b.WriteByte(s[i])
			i++
			continue
		}

		switch s[i+1] {
		case '$':
			// "$$" is make's escape for a literal dollar handed to the shell.
			b.WriteString("$")
			static = false
			i += 2
		case '(', '{':
			inner, end, ok := matchMakeExpr(s, i+1)
			if !ok {
				b.WriteByte(s[i])
				i++
				continue
			}
			text, ok := e.evalExpr(inner, s[i:end+1], keepVars, depth)
			b.WriteString(text)
			static = static && ok
			i = end + 1
		default:
			// Automatic variables ($@, $<, $^) are handled by the caller.
			b.WriteByte(s[i])
			i++
		}
	}

	return b.String(), static
}

// evalExpr evaluates the contents of a single $(...) expression. raw is the
// expression including its "$(" and ")" delimiters.
func (e *makeEval) evalExpr(inner, raw string, keepVars bool, depth int) (string, bool) {
	name, rest := splitMakeFuncName(inner)

	switch {
	case name == "shell":
		cmd, _ := e.expand(strings.TrimSpace(rest), false, depth+1)
		return e.protect("$(" + cmd + ")"), false

	case name == "wildcard":
		// Shell globbing is the closest runtime equivalent.
		pattern, _ := e.expand(strings.TrimSpace(rest), false, depth+1)
		e.evaluated = true
		e.notes = append(e.notes, "$(wildcard "+pattern+") became the shell glob "+pattern)
		return pattern, false

	case name == "realpath", name == "abspath":
		arg, _ := e.expand(strings.TrimSpace(rest), false, depth+1)
		flags := ""
		if name == "abspath" {
			flags = "-ms " // abspath does not resolve symlinks
		}
		e.evaluated = true
		e.notes = append(e.notes, "$("+name+" "+arg+") became a realpath(1) call")
		return e.protect("$(realpath " + flags + arg + ")"), false

	case name == "if":
		// Only the taken branch matters, so a dynamic branch is still fine as
		// long as the condition itself resolves.
		args := splitMakeArgs(rest, 3)
		if len(args) >= 2 {
			if cond, ok := e.expand(args[0], false, depth+1); ok {
				e.evaluated = true
				if strings.TrimSpace(cond) != "" {
					return e.expand(args[1], false, depth+1)
				}
				if len(args) == 3 {
					return e.expand(args[2], false, depth+1)
				}
				return "", true
			}
		}
		return e.reject(raw)

	case makeFuncUnsupported[name]:
		return e.reject(raw)
	}

	if arity, isFunc := makeFuncArity[name]; isFunc {
		args := splitMakeArgs(rest, arity)
		expanded := make([]string, len(args))
		for i, a := range args {
			text, ok := e.expand(a, false, depth+1)
			if !ok {
				return e.reject(raw)
			}
			expanded[i] = text
		}
		out, ok := applyMakeFunc(name, expanded)
		if !ok {
			return e.reject(raw)
		}
		e.evaluated = true
		return out, true
	}

	varName := strings.TrimSpace(inner)
	_, declared := e.vars[varName]
	if !isMakeVarName(varName) && !declared {
		// Not a make construct at all — e.g. "$(date +%Y)" in a justfile
		// recipe, which is already shell command substitution. A name make
		// would reject is only treated as a variable when the source actually
		// defined one, so "$(update-caches)" converts and "$(some-command)"
		// is still left for the shell.
		return e.protect(raw), false
	}
	if keepVars {
		if !isMakeVarName(varName) {
			// The $(VAR) → {{var}} pass in convertVarRefs only recognises
			// identifier names, so a define'd macro like "update-caches" has
			// to become its placeholder here or it reaches bash as command
			// substitution and dies with "command not found".
			return e.protect("{{" + SanitizeVarName(varName) + "}}"), false
		}
		return raw, false
	}
	if e.expanding[varName] {
		return "", false // recursive variable; give up rather than loop
	}
	if !declared {
		return "", true // undefined variables expand to empty, as in make
	}
	value := e.vars[varName]
	e.expanding[varName] = true
	defer delete(e.expanding, varName)
	e.usedVars[varName] = true
	return e.expand(value, false, depth+1)
}

// reject records an expression the migrator cannot convert and hands it back
// untouched (protected, so later passes leave it alone). Only the outermost
// expression is kept: reporting every nested function that failed with it is
// noise, since fixing the outer one fixes them all.
func (e *makeEval) reject(raw string) (string, bool) {
	kept := e.unsupported[:0]
	for _, u := range e.unsupported {
		if !strings.Contains(raw, u) {
			kept = append(kept, u)
		}
	}
	e.unsupported = append(kept, raw)
	return e.protect(raw), false
}

// matchMakeExpr returns the text inside the bracket opened at index open,
// along with the index of its closing bracket.
func matchMakeExpr(s string, open int) (string, int, bool) {
	var openCh, closeCh byte
	switch s[open] {
	case '(':
		openCh, closeCh = '(', ')'
	case '{':
		openCh, closeCh = '{', '}'
	default:
		return "", 0, false
	}

	depth := 1
	for i := open + 1; i < len(s); i++ {
		switch s[i] {
		case openCh:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				return s[open+1 : i], i, true
			}
		}
	}
	return "", 0, false
}

// splitMakeFuncName splits "strip $(FOO)" into its function name and
// arguments. Make requires whitespace after the name, so an expression
// without any is a variable reference and yields an empty name.
func splitMakeFuncName(inner string) (string, string) {
	for i := 0; i < len(inner); i++ {
		if inner[i] == ' ' || inner[i] == '\t' {
			return inner[:i], inner[i+1:]
		}
	}
	return "", ""
}

// splitMakeArgs splits a make argument list on top-level commas. At most
// max-1 splits are made (max <= 0 splits everything), so the final argument
// of a fixed-arity function may contain commas.
func splitMakeArgs(s string, max int) []string {
	var args []string
	depth := 0
	start := 0

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '{':
			depth++
		case ')', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 && (max <= 0 || len(args) < max-1) {
				args = append(args, s[start:i])
				start = i + 1
			}
		}
	}
	return append(args, s[start:])
}

// isMakeVarName reports whether s looks like a bare variable name.
func isMakeVarName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// applyMakeFunc evaluates a make text function over already-expanded
// arguments. It reports false when the call is malformed.
func applyMakeFunc(name string, args []string) (string, bool) {
	if arity := makeFuncArity[name]; arity > 0 && len(args) != arity {
		return "", false
	}

	switch name {
	case "strip":
		return strings.Join(strings.Fields(args[0]), " "), true
	case "subst":
		return strings.ReplaceAll(args[2], args[0], args[1]), true
	case "patsubst":
		return mapWords(args[2], func(w string) string { return patsubstWord(args[0], args[1], w) }), true
	case "filter":
		return filterWords(args[1], strings.Fields(args[0]), true), true
	case "filter-out":
		return filterWords(args[1], strings.Fields(args[0]), false), true
	case "findstring":
		if strings.Contains(args[1], args[0]) {
			return args[0], true
		}
		return "", true
	case "sort":
		return sortUniqueWords(args[0]), true
	case "words":
		return strconv.Itoa(len(strings.Fields(args[0]))), true
	case "word":
		n, err := strconv.Atoi(strings.TrimSpace(args[0]))
		if err != nil || n < 1 {
			return "", false
		}
		words := strings.Fields(args[1])
		if n > len(words) {
			return "", true
		}
		return words[n-1], true
	case "wordlist":
		from, err1 := strconv.Atoi(strings.TrimSpace(args[0]))
		to, err2 := strconv.Atoi(strings.TrimSpace(args[1]))
		if err1 != nil || err2 != nil || from < 1 || to < 0 {
			return "", false
		}
		words := strings.Fields(args[2])
		if from > len(words) || to < from {
			return "", true
		}
		if to > len(words) {
			to = len(words)
		}
		return strings.Join(words[from-1:to], " "), true
	case "firstword":
		if words := strings.Fields(args[0]); len(words) > 0 {
			return words[0], true
		}
		return "", true
	case "lastword":
		if words := strings.Fields(args[0]); len(words) > 0 {
			return words[len(words)-1], true
		}
		return "", true
	case "dir":
		return mapWords(args[0], func(w string) string {
			if i := strings.LastIndex(w, "/"); i >= 0 {
				return w[:i+1]
			}
			return "./"
		}), true
	case "notdir":
		return mapWords(args[0], func(w string) string {
			if i := strings.LastIndex(w, "/"); i >= 0 {
				return w[i+1:]
			}
			return w
		}), true
	case "suffix":
		return mapWords(args[0], wordSuffix), true
	case "basename":
		return mapWords(args[0], func(w string) string {
			return strings.TrimSuffix(w, wordSuffix(w))
		}), true
	case "addsuffix":
		return mapWords(args[1], func(w string) string { return w + args[0] }), true
	case "addprefix":
		return mapWords(args[1], func(w string) string { return args[0] + w }), true
	case "join":
		return joinWords(strings.Fields(args[0]), strings.Fields(args[1])), true
	case "and":
		last := ""
		for _, a := range args {
			if strings.TrimSpace(a) == "" {
				return "", true
			}
			last = a
		}
		return last, true
	case "or":
		for _, a := range args {
			if strings.TrimSpace(a) != "" {
				return a, true
			}
		}
		return "", true
	}

	return "", false
}

// mapWords applies fn to every whitespace-separated word, dropping empty
// results the way make's word-wise functions do.
func mapWords(list string, fn func(string) string) string {
	var out []string
	for _, w := range strings.Fields(list) {
		if r := fn(w); r != "" {
			out = append(out, r)
		}
	}
	return strings.Join(out, " ")
}

// wordSuffix returns the file suffix of a word, including the dot, or "".
func wordSuffix(w string) string {
	base := w
	if i := strings.LastIndex(w, "/"); i >= 0 {
		base = w[i+1:]
	}
	if i := strings.LastIndex(base, "."); i > 0 {
		return base[i:]
	}
	return ""
}

// patsubstWord applies a single $(patsubst pattern,replacement,word) rule.
func patsubstWord(pattern, replacement, word string) string {
	stem, ok := patMatch(pattern, word)
	if !ok {
		return word
	}
	if i := strings.Index(replacement, "%"); i >= 0 {
		return replacement[:i] + stem + replacement[i+1:]
	}
	return replacement
}

// patMatch matches a word against a make pattern, returning the text matched
// by "%". Patterns without a "%" must match exactly.
func patMatch(pattern, word string) (string, bool) {
	i := strings.Index(pattern, "%")
	if i < 0 {
		return "", pattern == word
	}
	prefix, suffix := pattern[:i], pattern[i+1:]
	if len(word) < len(prefix)+len(suffix) {
		return "", false
	}
	if !strings.HasPrefix(word, prefix) || !strings.HasSuffix(word, suffix) {
		return "", false
	}
	return word[len(prefix) : len(word)-len(suffix)], true
}

// filterWords keeps (or drops) the words of list matching any pattern.
func filterWords(list string, patterns []string, keep bool) string {
	var out []string
	for _, w := range strings.Fields(list) {
		matched := false
		for _, p := range patterns {
			if _, ok := patMatch(p, w); ok {
				matched = true
				break
			}
		}
		if matched == keep {
			out = append(out, w)
		}
	}
	return strings.Join(out, " ")
}

// sortUniqueWords implements $(sort list): lexical order, duplicates removed.
func sortUniqueWords(list string) string {
	words := strings.Fields(list)
	sort.Strings(words)
	out := words[:0]
	for i, w := range words {
		if i == 0 || w != words[i-1] {
			out = append(out, w)
		}
	}
	return strings.Join(out, " ")
}

// joinWords implements $(join list1,list2): pairwise concatenation.
func joinWords(a, b []string) string {
	n := max(len(a), len(b))
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		var word string
		if i < len(a) {
			word += a[i]
		}
		if i < len(b) {
			word += b[i]
		}
		out = append(out, word)
	}
	return strings.Join(out, " ")
}
