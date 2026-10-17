package server

import (
	"fmt"
	"net"
)

type Server struct {
	listener *net.Listener
}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) Start(port int) error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}

		go handleConnection(conn)
	}
}

func (s *Server) Stop() error {
	return (*s.listener).Close()
}

func handleConnection(conn net.Conn) {

}
