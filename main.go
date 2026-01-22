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

// ClientSession represents an active client connection with SNAT mapping
type ClientSession struct {
	clientAddr     *net.UDPAddr  // Original client address
	toServerConn   *net.UDPConn  // Connection to WireGuard server (has ephemeral port)
	toClientConn   *net.UDPConn  // Connection back to client (bound to listen port)
	lastActive     time.Time
	mu             sync.Mutex
}

// Relay manages UDP packet forwarding with SNAT
type Relay struct {
	listenAddr       string
	listenPort       int
	targetAddr       string
	timeout          time.Duration
	bufferSize       int
	dnsCheckInterval time.Duration
	listenConn       *net.UDPConn      // Main listening connection
	sessions         map[string]*ClientSession  // Keyed by client address
	sessionsMu       sync.RWMutex
	targetConn       *net.UDPAddr
	targetConnMu     sync.RWMutex
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
	listenAddr, err := net.ResolveUDPAddr("udp", r.listenAddr)
	if err != nil {
		return err
	}
	
	listenConn, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		return err
	}
	defer listenConn.Close()
	
	r.listenConn = listenConn
	r.listenPort = listenAddr.Port

	log.Printf("UDP relay started: %s -> %s (%s)", r.listenAddr, r.targetAddr, targetAddr.IP.String())
	log.Printf("Settings: timeout=%s, buffer=%d bytes, DNS check interval=%s", r.timeout, r.bufferSize, r.dnsCheckInterval)

	// Start DNS monitoring goroutine
	go r.monitorDNS()

	// Start session cleanup goroutine
	go r.cleanupSessions()

	// Main packet handling loop
	buffer := make([]byte, r.bufferSize)
	for {
		n, clientAddr, err := listenConn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("Error reading from client: %v", err)
			continue
		}

		// Make a copy of the packet data for the goroutine
		dataCopy := make([]byte, n)
		copy(dataCopy, buffer[:n])

		// Handle packet in goroutine for concurrency
		go r.handleClientPacket(dataCopy, clientAddr)
	}
}

// handleClientPacket processes a packet from a client with SNAT
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

		// Create connection TO server (gets ephemeral source port)
		toServerConn, err := net.DialUDP("udp", nil, targetConn)
		if err != nil {
			log.Printf("Error creating server connection for %s: %v", clientKey, err)
			r.sessionsMu.Unlock()
			return
		}

		// Create connection TO client (bound to our listen port for proper source)
		localAddr := &net.UDPAddr{
			IP:   net.IPv4zero,
			Port: r.listenPort,
		}
		toClientConn, err := net.DialUDP("udp", localAddr, clientAddr)
		if err != nil {
			log.Printf("Error creating client connection for %s: %v", clientKey, err)
			toServerConn.Close()
			r.sessionsMu.Unlock()
			return
		}

		session = &ClientSession{
			clientAddr:   clientAddr,
			toServerConn: toServerConn,
			toClientConn: toClientConn,
			lastActive:   time.Now(),
		}
		r.sessions[clientKey] = session

		log.Printf("[%s] New session: %s -> ephemeral:%d -> %s", 
			r.listenAddr, clientKey, toServerConn.LocalAddr().(*net.UDPAddr).Port, targetConn.String())

		// Start goroutine to handle responses from target
		go r.handleTargetResponses(session, clientKey)
	}
	r.sessionsMu.Unlock()

	// Update last active time
	session.mu.Lock()
	session.lastActive = time.Now()
	session.mu.Unlock()

	// SNAT: Forward packet to server through ephemeral port connection
	// Server sees: (relay_ip, ephemeral_port) -> (server_ip, server_port)
	_, err := session.toServerConn.Write(data)
	if err != nil {
		log.Printf("Error forwarding to target for %s: %v", clientKey, err)
	}
}

// handleTargetResponses reads responses from target and sends back to client with reverse SNAT
func (r *Relay) handleTargetResponses(session *ClientSession, clientKey string) {
	buffer := make([]byte, r.bufferSize)

	for {
		session.toServerConn.SetReadDeadline(time.Now().Add(r.timeout))
		n, err := session.toServerConn.Read(buffer)
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

		// Reverse SNAT: Send back to client from our listen port
		// Client sees: (relay_ip, listen_port) -> (client_ip, client_port)
		_, err = session.toClientConn.Write(buffer[:n])
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
		session.toServerConn.Close()
		session.toClientConn.Close()
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
				session.toServerConn.Close()
				session.toClientConn.Close()
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

		// Close old server connection
		oldConn := session.toServerConn
		oldConn.Close()

		// Create new connection to new target
		newConn, err := net.DialUDP("udp", nil, newTarget)
		if err != nil {
			log.Printf("[%s] Failed to migrate session %s: %v", r.listenAddr, clientKey, err)
			// Also close client connection and remove session
			session.toClientConn.Close()
			delete(r.sessions, clientKey)
			session.mu.Unlock()
			continue
		}

		// Update session with new connection
		session.toServerConn = newConn
		session.mu.Unlock()

		log.Printf("[%s] Migrated session: %s", r.listenAddr, clientKey)
		
		// Restart response handler for new connection
		go r.handleTargetResponses(session, clientKey)
	}
}
