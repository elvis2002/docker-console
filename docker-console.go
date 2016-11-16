package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	app "./console"
)

func main() {
	var address *string = flag.String("Address", "", "server listen address")
	var port *string = flag.String("Port", "8080", "server listen port")
	var sessionKey *string = flag.String("SessionKey", "_auth_user_id", "user serssion key")
	flag.Parse()
	options := app.DefaultOptions
	options.Address = *address
	options.Port = *port
	options.SessionKey = *sessionKey
	
	app, err := app.New(nil, &options)
	registerSignals(app)
	err = app.Run()
	if err != nil {
		exit(err, 4)
	}
	fmt.Println("Start server on " + options.Port + " success")
}

func exit(err error, code int) {
	if err != nil {
		fmt.Println(err)
	}
	os.Exit(code)
}

func registerSignals(app *app.App) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(
		sigChan,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	go func() {
		for {
			s := <-sigChan
			switch s {
			case syscall.SIGINT, syscall.SIGTERM:
				if app.Exit() {
					fmt.Println("Send ^C to force exit.")
				} else {
					os.Exit(5)
				}
			}
		}
	}()
}
