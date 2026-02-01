package main

import (
	"fmt"
	"github.com/javanhut/imlazy/parser"
	"log"
	"os"
)

func main() {
	var VersionNumber string = "0.1.0"
	cfg := parser.Config{}
	info, err := cfg.ReadToml()
	if err != nil {
		log.Fatal(fmt.Sprintf("Error: %v", err))
	}
	command := os.Args[1]
	if command == "how" || command == "help" {
		info.PrintCommands()
		verStr := "version"
		initialCmd := "init"
		fmt.Printf("  %-10s prints out the commands list\n", command)
		fmt.Printf("  %-10s returns the version of ImLazy\n", verStr)
		fmt.Printf("  %-10s initializes a lazy.toml in the root directory\n", initialCmd)
	} else if command == "--version" || command == "-v" || command == "version" {
		fmt.Println("ImLazy Version: ", VersionNumber)

	} else if command == "init" {
		info.InitialCommand()
	} else {
		if err := info.RunCommand(command); err != nil {
			log.Fatalf("Error: %v", err)
		}
	}
}
