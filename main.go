package main


import (
	"os"
	"fmt"
	"log"
	"github.com/javanhut/imlazy/parser"
)




func main() {
	cfg := parser.Config{}
	log.Println("Attempting to parse toml")
	info, err := cfg.ReadToml()
	if err != nil {
		log.Fatal(fmt.Sprintf("Error: %v", err))
	}
	command := os.Args[1]
	if command == "how" || command == "help" {
		info.PrintCommands()
		fmt.Printf("  %-10s prints out the commands list", command)
	} else {
	cmd, ok := info.GetCommand(command)
	if !ok {
		log.Fatalf("unknown command: %s", command)
	}
	log.Println(cmd.Desc)
	}
}
