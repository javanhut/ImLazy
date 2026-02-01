package main


import (
	"os"
	"fmt"
	"log"
	"github.com/javanhut/imlazy/parser"
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
		fmt.Printf("  %-10s prints out the commands list\n", command)
		fmt.Printf("  %-10s returns the version of ImLazy\n", verStr)
	} else if command == "--version" || command == "-v" || command == "version"{
		fmt.Println("ImLazy Version: ", VersionNumber)

	} else {
	info.RunCommand(command)
}
}
