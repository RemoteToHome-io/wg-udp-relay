package main

import (
	"flag"
	"log"
	"net"
	"sync"
	"time"
)

// ClientSession represents an active client connection
type ClientSession struct {
	clientAddr *net.UDPAddr
	conn       *net.UDPConn
	lastActive time.Time
	mu         sync.Mutex
}

// Relay manages UDP packet forwarding
type Relay struct {
	listenAddr  string
	targetAddr  string
	timeout     time.Duration
	bufferSize  int
	sessions    map[string]*ClientSession
	sessionsMu  sync.RWMutex
	targetConn  *net.UDPAddr
}

func main() {
	listenAddr := flag.String("listen", ":51820", "Address to listen on")
	targetAddr := flag.String("target", "", "Target WireGuard server address (required)")
	timeout := flag.Duration("timeout", 3*time.Minute, "Connection idle timeout")
	bufferSize := flag.Int("buffer", 1500, "UDP buffer size in bytes")
	
	flag.Parse()

	if *targetAddr == "" {
		log.Fatal("Error: -target flag is required")
	}

	relay := &Relay{
		listenAddr: *listenAddr,
		targetAddr: *targetAddr,
		timeout:    *timeout,
		bufferSize: *bufferSize,
		sessions:   make(map[string]*ClientSession),
	}

	if err := relay.Start(); err != nil {
		log.Fatalf("Failed to start relay: %v", err)
	}
}

// Start begins the relay server
func (r *Relay) Start() error {
	// Resolve target address
	targetAddr, err := net.ResolveUDPAddr("udp", r.targetAddr)
	if err != nil {
		return err
	}
	r.targetConn = targetAddr

	// Create listening socket
	listenConn, err := net.ListenPacket("udp", r.listenAddr)
	if err != nil {
		return err
	}
	defer listenConn.Close()

	log.Printf("UDP relay started: %s -> %s", r.listenAddr, r.targetAddr)
	log.Printf("Settings: timeout=%s, buffer=%d bytes", r.timeout, r.bufferSize)

	// Start session cleanup goroutine
	go r.cleanupSessions()

	// Main packet handling loop
	buffer := make([]byte, r.bufferSize)
	for {
		n, clientAddr, err := listenConn.ReadFrom(buffer)
		if err != nil {
			log.Printf("Error reading from client: %v", err)
			continue
		}

		// Handle packet in goroutine for concurrency
		go r.handleClientPacket(buffer[:n], clientAddr.(*net.UDPAddr))
	}
}

// handleClientPacket processes a packet from a client
func (r *Relay) handleClientPacket(data []byte, clientAddr *net.UDPAddr) {
	clientKey := clientAddr.String()

	// Get or create session
	r.sessionsMu.Lock()
	session, exists := r.sessions[clientKey]
	if !exists {
		// Create new session
		conn, err := net.DialUDP("udp", nil, r.targetConn)
		if err != nil {
			log.Printf("Error creating connection for %s: %v", clientKey, err)
			r.sessionsMu.Unlock()
			return
		}

		session = &ClientSession{
			clientAddr: clientAddr,
			conn:       conn,
			lastActive: time.Now(),
		}
		r.sessions[clientKey] = session

		log.Printf("New session: %s", clientKey)

		// Start goroutine to handle responses from target
		go r.handleTargetResponses(session, clientKey)
	}
	r.sessionsMu.Unlock()

	// Update last active time
	session.mu.Lock()
	session.lastActive = time.Now()
	session.mu.Unlock()

	// Forward packet to target
	_, err := session.conn.Write(data)
	if err != nil {
		log.Printf("Error forwarding to target for %s: %v", clientKey, err)
	}
}

// handleTargetResponses reads responses from target and sends back to client
func (r *Relay) handleTargetResponses(session *ClientSession, clientKey string) {
	buffer := make([]byte, r.bufferSize)
	
	// Create a connection back to the client
	listenConn, err := net.ListenPacket("udp", "")
	if err != nil {
		log.Printf("Error creating response listener for %s: %v", clientKey, err)
		return
	}
	defer listenConn.Close()

	for {
		session.conn.SetReadDeadline(time.Now().Add(r.timeout))
		n, err := session.conn.Read(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Printf("Session timeout: %s", clientKey)
			} else {
				log.Printf("Error reading from target for %s: %v", clientKey, err)
			}
			r.closeSession(clientKey)
			return
		}

		// Update last active time
		session.mu.Lock()
		session.lastActive = time.Now()
		session.mu.Unlock()

		// Send response back to client
		_, err = listenConn.WriteTo(buffer[:n], session.clientAddr)
		if err != nil {
			log.Printf("Error sending to client %s: %v", clientKey, err)
		}
	}
}

// closeSession closes and removes a client session
func (r *Relay) closeSession(clientKey string) {
	r.sessionsMu.Lock()
	defer r.sessionsMu.Unlock()

	if session, exists := r.sessions[clientKey]; exists {
		session.conn.Close()
		delete(r.sessions, clientKey)
		log.Printf("Closed session: %s", clientKey)
	}
}

// cleanupSessions periodically removes expired sessions
func (r *Relay) cleanupSessions() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		r.sessionsMu.Lock()
		for key, session := range r.sessions {
			session.mu.Lock()
			if now.Sub(session.lastActive) > r.timeout {
				session.conn.Close()
				delete(r.sessions, key)
				log.Printf("Cleaned up expired session: %s", key)
			}
			session.mu.Unlock()
		}
		r.sessionsMu.Unlock()
	}
}
