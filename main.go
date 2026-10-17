package main

import (
	"github.com/aeon022/mailctl/cmd"
	"github.com/aeon022/missionctl-core/secure"
)

func main() {
	secure.Init("mailctl") // owner-only files from the first byte
	cmd.Execute()
}
