package main

import (
	"github.com/shinyvision/vimfony/internal/server"
	"github.com/tliron/commonlog"
	_ "github.com/tliron/commonlog/simple"
)

func main() {
	commonlog.Configure(1, nil)

	s := server.NewServer()
	s.Run()
}

