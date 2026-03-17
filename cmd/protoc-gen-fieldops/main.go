package main

import (
	"github.com/lfreixial/proto-opscope/internal/generator"
	"google.golang.org/protobuf/compiler/protogen"
)

func main() {
	protogen.Options{}.Run(generator.Generate)
}
