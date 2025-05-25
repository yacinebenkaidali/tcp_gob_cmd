package cmmanager

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	actions "github.com/yacinebenkaidali/tcp_gob_server/commands"
)

type Connection struct {
	ID          string
	conn        net.Conn
	Address     string
	ConnectedAt time.Time
	LastPing    time.Time
	ctx         context.Context
	cancel      context.CancelFunc
}

type ConnectionManager struct {
	Connections sync.Map
	listener    net.Listener
	cancel      context.CancelFunc
	ctx         context.Context
	wg          sync.WaitGroup

	// Configuration
	pingInterval time.Duration
	readTimeout  time.Duration
	writeTimeout time.Duration

	onConnect    func(conn *net.Conn)
	onMessage    func(conn *net.Conn, cmd actions.Command)
	onDisconnect func(conn *net.Conn, err error)
}

type ConnectionConfig struct {
	// Configuration
	PingInterval time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	OnConnect    func(conn *net.Conn)
	OnMessage    func(conn *net.Conn, cmd actions.Command)
	OnDisconnect func(conn *net.Conn, err error)
}

func NewConnectionManager(config *ConnectionConfig) *ConnectionManager {
	ctx, cancel := context.WithCancel(context.Background())

	cm := &ConnectionManager{
		ctx:    ctx,
		cancel: cancel,
		wg:     sync.WaitGroup{},
	}

	// Set defaults or use provided config
	if config != nil {
		cm.pingInterval = config.PingInterval
		cm.readTimeout = config.ReadTimeout
		cm.writeTimeout = config.WriteTimeout
		cm.onConnect = config.OnConnect
		cm.onDisconnect = config.OnDisconnect
		cm.onMessage = config.OnMessage
	}

	// Set default values if not provided
	if cm.pingInterval == 0 {
		cm.pingInterval = 30 * time.Second
	}
	if cm.readTimeout == 0 {
		cm.readTimeout = 60 * time.Second
	}
	if cm.writeTimeout == 0 {
		cm.writeTimeout = 30 * time.Second
	}

	if config.OnConnect != nil {
		cm.onConnect = config.OnConnect
	}

	if config.OnMessage != nil {
		cm.onMessage = config.OnMessage
	}

	if config.OnDisconnect != nil {
		cm.onDisconnect = config.OnDisconnect
	}
	return cm
}

func (cm *ConnectionManager) StartServer(address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	cm.listener = listener

	cm.wg.Add(1)
	log.Printf("Server started on %s", address)
	go cm.acceptConnections()
	return nil
}

func (cm *ConnectionManager) acceptConnections() {
	defer func() {
		cm.wg.Done()
	}()

	for {
		select {
		case <-cm.ctx.Done():
			return
		default:
			{
				conn, err := cm.listener.Accept()
				if err != nil {
					if cm.ctx.Err() != err {
						return
					}
					log.Printf("There was an error establishing connection %v\n", err)
					continue
				}
				connectionId := fmt.Sprintf("incoming_conn_%s_%d", conn.RemoteAddr(), time.Now().UnixNano())
				cm.handleConnection(conn, connectionId, conn.RemoteAddr().String())

			}
		}
	}
}

func (cm *ConnectionManager) handleConnection(conn net.Conn, id string, address string) {
	ctx, cancel := context.WithCancel(cm.ctx)
	newConn := &Connection{
		ID:          id,
		conn:        conn,
		Address:     address,
		ctx:         ctx,
		cancel:      cancel,
		ConnectedAt: time.Now(),
		LastPing:    time.Now(),
	}
	cm.Connections.Store(id, newConn)

	if cm.onConnect != nil {
		cm.onConnect(&conn)
	}

	cm.wg.Add(3)
	go cm.handlePing(newConn)
	go cm.handleConnectionRead(newConn)
	go cm.handleMonitoringConnection(newConn)
}

func (cm *ConnectionManager) handleConnectionRead(conn *Connection) {
	defer func() {
		cm.wg.Done()
		cm.closeConnection(conn, nil)
	}()

	for {
		select {
		case <-conn.ctx.Done():
			return
		default:
			{
				lengthPrefixBytes := make([]byte, 4)
				_, err := io.ReadFull(conn.conn, lengthPrefixBytes)
				if err != nil {
					if err == io.EOF || err == io.ErrUnexpectedEOF {
						log.Printf("Client disconnected: %s", (conn.conn).RemoteAddr())
						return
					}
					log.Printf("Error reading length prefix from %s: %v", (conn.conn).RemoteAddr(), err)
					return
				}
				commandLength := binary.BigEndian.Uint32(lengthPrefixBytes)
				// Basic sanity check for command length (optional but recommended)
				if commandLength > 10*1024*1024 { // e.g., max 10MB command
					log.Printf("Command length %d exceeds maximum allowed size from %s", commandLength, conn.conn.RemoteAddr())
					return // Or send an error response
				}
				if commandLength == 0 {
					log.Printf("Received zero length command from %s, potentially keep-alive or error.", conn.conn.RemoteAddr())
					continue // Decide how to handle this; could be a protocol-specific keep-alive
				}
				commandBytes := make([]byte, commandLength)
				_, err = io.ReadFull(conn.conn, commandBytes)
				if err != nil {
					if err == io.EOF || err == io.ErrUnexpectedEOF {
						log.Printf("Client disconnected while reading command body: %s", conn.conn.RemoteAddr())
					} else {
						log.Printf("Error reading command body from %s: %v", conn.conn.RemoteAddr(), err)
					}
					return
				}
				var cmd actions.Command
				docoder := gob.NewDecoder(bytes.NewReader(commandBytes))
				if err := docoder.Decode(&cmd); err != nil {
					log.Printf("Error gob decoding command from %s: %v", conn.conn.RemoteAddr(), err)
					// Optionally, you might try to recover or just close the connection
					continue // or return
				}
				conn.LastPing = time.Now()
				if cm.onMessage != nil {
					cm.onMessage(&conn.conn, cmd)
				}
			}
		}
	}
}

func (cm *ConnectionManager) handlePing(conn *Connection) {
	defer cm.wg.Done()
	ticker := time.NewTicker(cm.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-cm.ctx.Done():
			return
		case <-ticker.C:
			{
				if err := cm.sendMessage(conn.ID, fmt.Append(nil, "PING")); err != nil {
					log.Printf("There was a problem pinging connection with id %s", conn.ID)
					return
				}
			}
		}
	}
}

func (cm *ConnectionManager) sendMessage(id string, data []byte) error {
	value, ok := cm.Connections.Load(id)
	if !ok {
		return fmt.Errorf("connection %s not found", id)
	}
	connection := (value).(*Connection)
	if err := connection.conn.SetWriteDeadline(time.Now().Add(cm.writeTimeout)); err != nil {
		return err
	}
	_, err := connection.conn.Write(data)
	return err
}

func (cm *ConnectionManager) handleMonitoringConnection(conn *Connection) {
	defer cm.wg.Done()
	ticker := time.NewTicker(cm.pingInterval * 2)
	defer ticker.Stop()
	for {
		select {
		case <-conn.ctx.Done():
			return
		case <-ticker.C:
			{
				if time.Since(conn.LastPing) > cm.pingInterval*3 {
					cm.closeConnection(conn, fmt.Errorf("connection with id %s appears to be unstable", conn.ID))
					return
				}
			}
		}
	}
}

func (cm *ConnectionManager) closeConnection(conn *Connection, err error) {
	// Ensure connection is only closed once
	if _, loaded := cm.Connections.LoadAndDelete(conn.ID); !loaded {
		// Connection was already closed
		return
	}

	// Close the actual connection first
	if err := conn.conn.Close(); err != nil {
		log.Printf("There was an error closing connection %v\n", err)
	}

	// Cancel the connection context after closing
	conn.cancel()

	if cm.onDisconnect != nil {
		cm.onDisconnect(&conn.conn, err)
	}

	log.Printf("Connection %s is closed.\n", conn.Address)
}

func (cm *ConnectionManager) Shutdown() {
	// Cancel the main context first
	cm.cancel()

	// Close the listener to stop accepting new connections
	if cm.listener != nil {
		if err := cm.listener.Close(); err != nil {
			log.Printf("Error closing listener: %v\n", err)
		}
	}

	// Close all existing connections
	cm.Connections.Range(func(key, value any) bool {
		connection := (value).(*Connection)
		cm.closeConnection(connection, fmt.Errorf("server shutdown initiated"))
		return true
	})

	// Wait for all goroutines to finish
	cm.wg.Wait()
	log.Println("Server shutdown completed")
}
