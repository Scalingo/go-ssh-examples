// Based on https://gist.github.com/jpillora/b480fde82bff51a06238
// A simple SSH server providing bash sessions
//
// Server:
// cd my/new/dir/
// ssh-keygen -t rsa #generate server keypair
// go get -v .
// go run sshd.go
//
// Client:
// ssh foo@localhost -p 2022

package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"github.com/kr/pty"
	"golang.org/x/crypto/ssh"
)

var (
	DEFAULT_SHELL string = "sh"
)

func main() {
	// An SSH server is represented by a ServerConfig, which holds
	// certificate details and handles authentication of ServerConns.
	sshConfig := &ssh.ServerConfig{
		NoClientAuth: true,
	}

	// You can generate a keypair with 'ssh-keygen -t rsa -C "test@example.com"'
	privateBytes, err := ioutil.ReadFile("./id_rsa")
	if err != nil {
		log.Fatal("Failed to load private key (./id_rsa)")
	}

	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		log.Fatal("Failed to parse private key")
	}

	sshConfig.AddHostKey(private)

	// Once a ServerConfig has been configured, connections can be accepted.
	listener, err := net.Listen("tcp4", ":2022")
	if err != nil {
		log.Fatalf("failed to listen on *:2022")
	}

	// Accept all connections
	log.Printf("listening on %s", ":2022")
	for {
		tcpConn, err := listener.Accept()
		if err != nil {
			log.Printf("failed to accept incoming connection (%s)", err)
			continue
		}
		// Before use, a handshake must be performed on the incoming net.Conn.
		sshConn, chans, reqs, err := ssh.NewServerConn(tcpConn, sshConfig)
		if err != nil {
			log.Printf("failed to handshake (%s)", err)
			continue
		}

		// Check remote address
		log.Printf("new ssh connection from %s (%s)", sshConn.RemoteAddr(), sshConn.ClientVersion())

		// Print incoming out-of-band Requests
		go handleRequests(reqs)
		// Accept all channels
		go handleChannels(chans)
	}
}

func handleRequests(reqs <-chan *ssh.Request) {
	for req := range reqs {
		log.Printf("recieved out-of-band request: %+v", req)
	}
}

// Start assigns a pseudo-terminal tty os.File to c.Stdin, c.Stdout,
// and c.Stderr, calls c.Start, and returns the File of the tty's
// corresponding pty.
func PtyRun(c *exec.Cmd, tty *os.File) (err error) {
	defer tty.Close()
	c.Stdout = tty
	c.Stdin = tty
	c.Stderr = tty
	c.SysProcAttr = &syscall.SysProcAttr{
		Setctty: true,
		Setsid:  true,
	}
	return c.Start()
}

func handleChannels(chans <-chan ssh.NewChannel) {
	// Service the incoming Channel channel.
	for newChannel := range chans {
		// Channels have a type, depending on the application level
		// protocol intended. In the case of a shell, the type is
		// "session" and ServerShell may be used to present a simple
		// terminal interface.
		if t := newChannel.ChannelType(); t != "session" {
			newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", t))
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			log.Printf("could not accept channel (%s)", err)
			continue
		}

		// allocate a terminal for this channel
		log.Print("creating pty...")
		// Create new pty
		f, tty, err := pty.Open()
		if err != nil {
			log.Printf("could not start pty (%s)", err)
			continue
		}

		var shell string
		shell = os.Getenv("SHELL")
		if shell == "" {
			shell = DEFAULT_SHELL
		}

		// Sessions have out-of-band requests such as "shell", "pty-req" and "env"
		go func(in <-chan *ssh.Request) {
			for req := range in {
				log.Printf("%v %s", req.Payload, req.Payload)
				ok := false
				switch req.Type {
				case "exec":
					ok = true
					command := string(req.Payload[4 : req.Payload[3]+4])
					cmd := exec.Command(shell, []string{"-c", command}...)

					cmd.Stdout = channel
					cmd.Stderr = channel
					cmd.Stdin = channel

					err := cmd.Start()
					if err != nil {
						log.Printf("could not start command (%s)", err)
						continue
					}

					// teardown session
					go func() {
						_, err := cmd.Process.Wait()
						if err != nil {
							log.Printf("failed to exit bash (%s)", err)
						}
						channel.Close()
						log.Printf("session closed")
					}()
				case "shell":
					cmd := exec.Command(shell)
					cmd.Env = []string{"TERM=xterm"}
					err := PtyRun(cmd, tty)
					if err != nil {
						log.Printf("%s", err)
					}

					// Teardown session
					var once sync.Once
					close := func() {
						channel.Close()
						log.Printf("session closed")
					}

					// Pipe session to bash and visa-versa
					go func() {
						io.Copy(channel, f)
						once.Do(close)
					}()

					go func() {
						io.Copy(f, channel)
						once.Do(close)
					}()

					// We don't accept any commands (Payload),
					// only the default shell.
					if len(req.Payload) == 0 {
						ok = true
					}
				case "pty-req":
					// Responding 'ok' here will let the client
					// know we have a pty ready for input
					ok = true
					// Parse body...
					termLen := req.Payload[3]
					termEnv := string(req.Payload[4 : termLen+4])
					w, h := parseDims(req.Payload[termLen+4:])
					SetWinsize(f.Fd(), w, h)
					log.Printf("pty-req '%s'", termEnv)
				case "window-change":
					w, h := parseDims(req.Payload)
					SetWinsize(f.Fd(), w, h)
					continue //no response
				}

				if !ok {
					log.Printf("declining %s request...", req.Type)
				}

				req.Reply(ok, nil)
			}
		}(requests)
	}
}

// =======================

// parseDims extracts two uint32s from the provided buffer.
func parseDims(b []byte) (uint32, uint32) {
	w := binary.BigEndian.Uint32(b)
	h := binary.BigEndian.Uint32(b[4:])
	return w, h
}

// Winsize stores the Height and Width of a terminal.
type Winsize struct {
	Height uint16
	Width  uint16
	x      uint16 // unused
	y      uint16 // unused
}

// SetWinsize sets the size of the given pty.
func SetWinsize(fd uintptr, w, h uint32) {
	log.Printf("window resize %dx%d", w, h)
	ws := &Winsize{Width: uint16(w), Height: uint16(h)}
	syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(ws)))
}
