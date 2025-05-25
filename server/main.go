package main

import (
	"encoding/gob"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	cmManager "github.com/yacinebenkaidali/tcp_gob_server/cm_manager"
	actions "github.com/yacinebenkaidali/tcp_gob_server/commands"
)

func init() {
	gob.Register(actions.Command{})
	gob.Register(map[string]int{})
}

func main() {
	commandsCh := make(chan actions.Command)

	cmConfig := cmManager.ConnectionConfig{
		OnConnect: func(conn *net.Conn) {
		},
		OnMessage: func(conn *net.Conn, cmd actions.Command) {
			commandsCh <- cmd
		},
	}
	cm := cmManager.NewConnectionManager(&cmConfig)
	quitCh := make(chan os.Signal, 1)
	signal.Notify(quitCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		for cmd := range commandsCh {
			log.Println("received this command after parsing", cmd)
		}
	}()

	cm.StartServer(":8000")

	select {
	case <-quitCh:
		{
			cm.Shutdown()
			log.Println("Received kill signal, closing off connection and server")
		}
	}
}
