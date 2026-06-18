package main

import (
	"armrecorder"

	sensor "go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/generic"
)

func main() {
	module.ModularMain(
		resource.APIModel{API: sensor.API, Model: armrecorder.Recorder},
		resource.APIModel{API: generic.API, Model: armrecorder.Reactor},
	)
}
