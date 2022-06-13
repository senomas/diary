package main

import (
	"fmt"
	"os"
)

func main() {
	journal := OpenJournal("/home/seno/journal/")
	switch len(os.Args) {
	case 1:
		journal.processChanges()
		journal.Write()
	case 2:
		switch os.Args[1] {
		case "index":
			journal.OpenIndex()
		case "new":
			journal.CreateDiary()
		case "push":
			journal.Push()
		case "all":
			journal.processAll()
			journal.Write()
		default:
			fmt.Printf("UNKNOWN COMMAND '%s'\n", os.Args[1])
		}
	default:
		fmt.Printf("ARGS %#v\n", os.Args)
	}
}
