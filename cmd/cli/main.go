package main

import (
	"armrecorder"
	"context"
	"fmt"
	"go.viam.com/rdk/logging"
)

func main() {
	err := realMain()
	if err != nil {
		panic(err)
	}
}

func realMain() error {
	ctx := context.Background()
	logger := logging.NewLogger("cli")
	_ = ctx
	_ = logger
	_ = armrecorder.Recorder
	fmt.Println("arm-recorder cli stub")
	return nil
}
