package main

import (
	"github.com/H-I-V-E-Tec/hive_atlas/internal/app"
	"os"
)

func main() { os.Exit(app.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
