package testonvif

import (
	"encoding/xml"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
)

type DiscoverySimulator struct {
	Address string
	Packets atomic.Int32
	Quiet   atomic.Bool
}

func StartDiscovery(t *testing.T, endpoint string) *DiscoverySimulator {
	t.Helper()
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	sim := &DiscoverySimulator{Address: socket.LocalAddr().String()}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 65536)
		for {
			n, peer, err := socket.ReadFromUDP(buf)
			if err != nil {
				return
			}
			sim.Packets.Add(1)
			if sim.Quiet.Load() {
				continue
			}
			var probe struct {
				Header struct {
					ID     string `xml:"http://schemas.xmlsoap.org/ws/2004/08/addressing MessageID"`
					Action string `xml:"http://schemas.xmlsoap.org/ws/2004/08/addressing Action"`
				} `xml:"http://www.w3.org/2003/05/soap-envelope Header"`
			}
			if xml.Unmarshal(buf[:n], &probe) != nil || probe.Header.ID == "" || probe.Header.Action != "http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe" {
				continue
			}
			_, _ = socket.WriteToUDP(DiscoveryMatch(probe.Header.ID+"-stale", endpoint), peer)
			body := DiscoveryMatch(probe.Header.ID, "https://192.0.2.1/foreign "+endpoint)
			_, _ = socket.WriteToUDP(body, peer)
			_, _ = socket.WriteToUDP(body, peer)
		}
	}()
	t.Cleanup(func() { socket.Close(); <-done })
	return sim
}

func DiscoveryMatch(requestID, addresses string) []byte {
	return []byte(fmt.Sprintf(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery" xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><s:Header><a:MessageID>urn:uuid:c92e16c2-12ac-4e48-af34-1547cdc8ca81</a:MessageID><a:RelatesTo>%s</a:RelatesTo><a:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/ProbeMatches</a:Action><d:AppSequence InstanceId="1" MessageNumber="1"/></s:Header><s:Body><d:ProbeMatches><d:ProbeMatch><a:EndpointReference><a:Address>urn:uuid:fb05c413-4130-4c09-ae80-792e90373d8a</a:Address></a:EndpointReference><d:Types>tds:Device</d:Types><d:Scopes>onvif://www.onvif.org/name/LocalCamera onvif://www.onvif.org/hardware/TestCamera</d:Scopes><d:XAddrs>%s</d:XAddrs><d:MetadataVersion>1</d:MetadataVersion></d:ProbeMatch></d:ProbeMatches></s:Body></s:Envelope>`, requestID, addresses))
}
