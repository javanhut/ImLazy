package parser 


import (
	"log"
	"github.com/BurntSushi/toml"
)


type TomlInfo struct {
	commandsList []string `toml:"commands"`
}


func (t *TomlInfo) readToml() {
	if _, err := toml.Decode(tomlData, &t); err != nil {
		log.Fatal(err)
	}
}
