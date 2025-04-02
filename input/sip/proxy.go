package sip

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/google/uuid"
	"github.com/icholy/digest"
	"log/slog"
	"sync"
)

const serverName = "HAE-Proxy"

type RegisteredUA struct {
	Challenge digest.Challenge
}

type Proxy struct {
	client    *sipgo.Client
	ua        *sipgo.UserAgent
	server    *sipgo.Server
	errors    chan error
	registrar map[string]*RegisteredUA
}

func NewProxy(name string) (*Proxy, error) {
	ua, err := sipgo.NewUA(sipgo.WithUserAgent(name))
	if err != nil {
		return nil, fmt.Errorf("could not init user agent: %w", err)
	}
	return &Proxy{
		ua:        ua,
		errors:    make(chan error),
		registrar: map[string]*RegisteredUA{},
	}, nil
}

func (p *Proxy) ListenUDP(ctx context.Context, addr string, wg *sync.WaitGroup) error {
	var err error
	p.server, err = sipgo.NewServer(p.ua)
	p.server.OnRegister(func(req *sip.Request, tx sip.ServerTransaction) {
		fmt.Println(req.String())
		ua := p.registrar[req.Contact().Name()]
		if ua == nil {
			ua = &RegisteredUA{
				Challenge: digest.Challenge{
					Realm:     serverName,
					Nonce:     uuid.New().String(),
					Algorithm: "MD5",
					QOP:       []string{"auth"},
				},
			}
			p.registrar[req.Contact().Name()] = ua
			res := sip.NewResponseFromRequest(req, sip.StatusUnauthorized, "Unauthorized", nil)
			res.AppendHeader(sip.NewHeader("WWW-Authenticate", ua.Challenge.String()))
			res.AppendHeader(sip.NewHeader("Server", serverName))
			fmt.Println(res.String())
			err = tx.Respond(res)
			if err != nil {
				p.error(fmt.Errorf("could not respond to REGISTER: %w", err))
			}
		}

		res := sip.NewResponseFromRequest(req, sip.StatusOK, "OK", nil)
		res.AppendHeader(sip.NewHeader("Expires", "3600"))
		res.AppendHeader(sip.NewHeader("Server", serverName))
		fmt.Println(res.String())
		err = tx.Respond(res)
		if err != nil {
			p.error(fmt.Errorf("could not respond to REGISTER: %w", err))
		}
	})
	p.server.OnInvite(func(req *sip.Request, tx sip.ServerTransaction) {
		fmt.Println(req.String())
		res := sip.NewResponseFromRequest(req, sip.StatusOK, "", nil)
		fmt.Println(res.String())
		hex.Dump(res.Body())
		err = tx.Respond(res)
		if err != nil {
			p.error(fmt.Errorf("could not respond to REGISTER: %w", err))
		}
	})
	p.server.OnBye(func(req *sip.Request, tx sip.ServerTransaction) {
		fmt.Println(req.String())
		err = tx.Respond(sip.NewResponseFromRequest(req, sip.StatusOK, "", nil))
		if err != nil {
			p.error(fmt.Errorf("could not respond to BYE: %w", err))
		}
	})
	p.server.OnCancel(func(req *sip.Request, tx sip.ServerTransaction) {
		fmt.Println(req.String())
		err = tx.Respond(sip.NewResponseFromRequest(req, sip.StatusOK, "", nil))
		if err != nil {
			p.error(fmt.Errorf("could not respond to CANCEL: %w", err))
		}
	})
	p.server.OnOptions(func(req *sip.Request, tx sip.ServerTransaction) {
		fmt.Println(req.String())
		err = tx.Respond(sip.NewResponseFromRequest(req, sip.StatusOK, "", nil))
		if err != nil {
			p.error(fmt.Errorf("could not respond to REGISTER: %w", err))
		}
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = p.server.ListenAndServe(ctx, "udp", addr)
		if err != nil {
			slog.Error("error running sip server", "err", err)
		}
	}()
	return nil
}

func (p *Proxy) error(err error) {
	select {
	case p.errors <- err:
	default:
		slog.Error("sip proxy error", "err", err)
	}
}
