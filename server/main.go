package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	cmManager "github.com/yacinebenkaidali/tcp_gob_server/cm_manager"
)

func main() {
	cmConfig := cmManager.ConnectionConfig{
		OnConnect: func(conn *net.Conn) {},
		OnMessage: func(conn *net.Conn, data []byte) {
			log.Printf("YOyo: %s", string(data))
		},
		OnDisconnect: func(conn *net.Conn, err error) {
			log.Printf("Connection with address %s has been closed", (*conn).RemoteAddr().String())
		},
	}
	cm := cmManager.NewConnectionManager(&cmConfig)
	quitCh := make(chan os.Signal, 1)
	signal.Notify(quitCh, os.Interrupt, syscall.SIGTERM)

	cm.StartServer(":8000")

	select {
	case <-quitCh:
		{
			cm.Shutdown()
			log.Println("Received kill signal, closing off connection and server")
		}
	}
}
