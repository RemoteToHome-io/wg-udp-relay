package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
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
	listenAddr      string
	targetAddr      string
	timeout         time.Duration
	bufferSize      int
	dnsCheckInterval time.Duration
	sessions        map[string]*ClientSession
	sessionsMu      sync.RWMutex
	targetConn      *net.UDPAddr
	targetConnMu    sync.RWMutex
}

func main() {
	listenPorts := flag.String("ports", "", "Comma-separated list of ports to listen on (e.g., 51820,51821)")
	targetAddr := flag.String("target", "", "Target WireGuard server address (required)")
	timeout := flag.Duration("timeout", 3*time.Minute, "Connection idle timeout")
	bufferSize := flag.Int("buffer", 1500, "UDP buffer size in bytes")
	dnsCheckInterval := flag.Duration("dns-check", 5*time.Minute, "DNS resolution check interval")
	
	flag.Parse()

	// Check for environment variables if flags not provided
	if *listenPorts == "" {
		*listenPorts = os.Getenv("LISTEN_PORTS")
	}
	if *targetAddr == "" {
		*targetAddr = os.Getenv("TARGET_ENDPOINT")
	}
	if envInterval := os.Getenv("DNS_CHECK_INTERVAL"); envInterval != "" && *dnsCheckInterval == 5*time.Minute {
		if parsed, err := time.ParseDuration(envInterval); err == nil {
			*dnsCheckInterval = parsed
		} else {
			log.Printf("Warning: Invalid DNS_CHECK_INTERVAL '%s', using default 5m", envInterval)
		}
	}

	if *targetAddr == "" {
		log.Fatal("Error: -target flag or TARGET_ENDPOINT environment variable is required")
	}

	if *listenPorts == "" {
		log.Fatal("Error: -ports flag or LISTEN_PORTS environment variable is required")
	}

	// Parse listen ports
	ports := strings.Split(*listenPorts, ",")
	if len(ports) == 0 {
		log.Fatal("Error: At least one listen port must be specified")
	}

	// Start a relay for each port
	var wg sync.WaitGroup
	for _, port := range ports {
		port = strings.TrimSpace(port)
		listenAddr := fmt.Sprintf(":%s", port)
		
		relay := &Relay{
			listenAddr:       listenAddr,
			targetAddr:       *targetAddr,
			timeout:          *timeout,
			bufferSize:       *bufferSize,
			dnsCheckInterval: *dnsCheckInterval,
			sessions:         make(map[string]*ClientSession),
		}

		wg.Add(1)
		go func(r *Relay) {
			defer wg.Done()
			if err := r.Start(); err != nil {
				log.Printf("Failed to start relay on %s: %v", r.listenAddr, err)
			}
		}(relay)
	}

	// Wait for all relays
	wg.Wait()
}

// Start begins the relay server
func (r *Relay) Start() error {
	// Resolve target address
	targetAddr, err := net.ResolveUDPAddr("udp", r.targetAddr)
	if err != nil {
		return err
	}
	r.targetConnMu.Lock()
	r.targetConn = targetAddr
	r.targetConnMu.Unlock()

	// Create listening socket
	listenConn, err := net.ListenPacket("udp", r.listenAddr)
	if err != nil {
		return err
	}
	defer listenConn.Close()

	log.Printf("UDP relay started: %s -> %s (%s)", r.listenAddr, r.targetAddr, targetAddr.IP.String())
	log.Printf("Settings: timeout=%s, buffer=%d bytes, DNS check interval=%s", r.timeout, r.bufferSize, r.dnsCheckInterval)

	// Start DNS monitoring goroutine
	go r.monitorDNS()

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
		// Get current target address
		r.targetConnMu.RLock()
		targetConn := r.targetConn
		r.targetConnMu.RUnlock()

		// Create new session
		conn, err := net.DialUDP("udp", nil, targetConn)
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

// monitorDNS periodically checks for DNS changes and updates target address
func (r *Relay) monitorDNS() {
	ticker := time.NewTicker(r.dnsCheckInterval)
	defer ticker.Stop()

	for range ticker.C {
		// Resolve target address
		newAddr, err := net.ResolveUDPAddr("udp", r.targetAddr)
		if err != nil {
			log.Printf("[%s] DNS resolution error for %s: %v", r.listenAddr, r.targetAddr, err)
			continue
		}

		// Check if IP has changed
		r.targetConnMu.RLock()
		currentAddr := r.targetConn
		r.targetConnMu.RUnlock()

		if !currentAddr.IP.Equal(newAddr.IP) || currentAddr.Port != newAddr.Port {
			log.Printf("[%s] DNS change detected: %s -> %s", r.listenAddr, currentAddr.IP.String(), newAddr.IP.String())
			
			// Update target address
			r.targetConnMu.Lock()
			r.targetConn = newAddr
			r.targetConnMu.Unlock()

			// Migrate all existing sessions to new target
			r.migrateSessionsToNewTarget(newAddr)
		}
	}
}

// migrateSessionsToNewTarget recreates all session connections to point to new target
func (r *Relay) migrateSessionsToNewTarget(newTarget *net.UDPAddr) {
	r.sessionsMu.Lock()
	defer r.sessionsMu.Unlock()

	log.Printf("[%s] Migrating %d sessions to new target %s", r.listenAddr, len(r.sessions), newTarget.IP.String())

	for clientKey, session := range r.sessions {
		session.mu.Lock()

		// Close old connection
		oldConn := session.conn
		oldConn.Close()

		// Create new connection to new target
		newConn, err := net.DialUDP("udp", nil, newTarget)
		if err != nil {
			log.Printf("[%s] Failed to migrate session %s: %v", r.listenAddr, clientKey, err)
			// Remove failed session
			delete(r.sessions, clientKey)
			session.mu.Unlock()
			continue
		}

		// Update session with new connection
		session.conn = newConn
		session.mu.Unlock()

		log.Printf("[%s] Migrated session: %s", r.listenAddr, clientKey)
	}
}
