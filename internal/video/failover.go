package video

import (
	"context"
	"errors"
	"sync"
	"time"
)

// failoverRise is how many consecutive healthy checks a preferred media
// server needs before the module moves back to it, so a flapping server
// does not interrupt playback on every check. The web proxy's HAProxy uses
// the same server order and a matching rise count.
const failoverRise = 3

type mediaMember struct {
	id, mediaIP string
	server      mediaServer
}

// failoverMedia runs the module on one of several media servers: the first
// healthy one in configured order. A switch loses the live streams of the
// previous server; onSwitch lets the module mark them lost and restart
// them on the new server.
type failoverMedia struct {
	members  []mediaMember
	onSwitch func(from, to mediaMember)

	mu     sync.Mutex
	active int
	streak []int
}

func newFailoverMedia(members []mediaMember) *failoverMedia {
	return &failoverMedia{members: members, streak: make([]int, len(members))}
}

func (f *failoverMedia) current() mediaMember {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.members[f.active]
}

// Health checks every server and moves to the first healthy one: at once
// when the active server is down, otherwise only to a preferred server that
// stayed healthy for failoverRise checks. It reports the active server.
func (f *failoverMedia) Health(ctx context.Context) error {
	// Probed together, so a hung server does not use up the caller's
	// deadline before the others are checked.
	healthy := make([]error, len(f.members))
	var wg sync.WaitGroup
	for i, m := range f.members {
		wg.Add(1)
		go func() {
			defer wg.Done()
			healthy[i] = m.server.Health(ctx)
		}()
	}
	wg.Wait()
	f.mu.Lock()
	for i, err := range healthy {
		if err == nil {
			f.streak[i]++
		} else {
			f.streak[i] = 0
		}
	}
	from := f.active
	target := from
	for i := range f.members {
		if healthy[i] != nil {
			continue
		}
		if healthy[from] != nil || (i < from && f.streak[i] >= failoverRise) {
			target = i
		}
		break
	}
	f.active = target
	f.mu.Unlock()
	if target != from && f.onSwitch != nil {
		f.onSwitch(f.members[from], f.members[target])
	}
	if healthy[target] != nil {
		return errors.Join(healthy...)
	}
	return nil
}

func (f *failoverMedia) AddStreamProxy(ctx context.Context, app, stream, sourceURL string, cred Credentials, opt proxyOptions) error {
	return f.current().server.AddStreamProxy(ctx, app, stream, sourceURL, cred, opt)
}
func (f *failoverMedia) DelStreamProxy(ctx context.Context, app, stream string) error {
	return f.current().server.DelStreamProxy(ctx, app, stream)
}
func (f *failoverMedia) ListStreamProxies(ctx context.Context) ([]string, error) {
	return f.current().server.ListStreamProxies(ctx)
}
func (f *failoverMedia) AddFFmpegSource(ctx context.Context, cmdKey, srcURL, dstURL string, timeout time.Duration) (string, error) {
	return f.current().server.AddFFmpegSource(ctx, cmdKey, srcURL, dstURL, timeout)
}
func (f *failoverMedia) DelFFmpegSource(ctx context.Context, key string) error {
	return f.current().server.DelFFmpegSource(ctx, key)
}
func (f *failoverMedia) ListFFmpegSources(ctx context.Context) ([]ffmpegSource, error) {
	return f.current().server.ListFFmpegSources(ctx)
}
func (f *failoverMedia) MediaInfo(ctx context.Context, app, stream string) (mediaInfo, bool, error) {
	return f.current().server.MediaInfo(ctx, app, stream)
}
func (f *failoverMedia) HLSReady(ctx context.Context, app, stream string) (bool, error) {
	return f.current().server.HLSReady(ctx, app, stream)
}
func (f *failoverMedia) CloseStreams(ctx context.Context, app, stream string) error {
	return f.current().server.CloseStreams(ctx, app, stream)
}
func (f *failoverMedia) WHEP(ctx context.Context, app, stream, token, offer string) (string, string, error) {
	return f.current().server.WHEP(ctx, app, stream, token, offer)
}
func (f *failoverMedia) DeleteWebRTC(ctx context.Context, deleteQuery string) error {
	return f.current().server.DeleteWebRTC(ctx, deleteQuery)
}
func (f *failoverMedia) OpenRTPServer(ctx context.Context, app, stream string, tcp bool, ssrc string) (int, error) {
	return f.current().server.OpenRTPServer(ctx, app, stream, tcp, ssrc)
}
func (f *failoverMedia) UpdateRTPServerSSRC(ctx context.Context, app, stream, ssrc string) error {
	return f.current().server.UpdateRTPServerSSRC(ctx, app, stream, ssrc)
}
func (f *failoverMedia) CloseRTPServer(ctx context.Context, app, stream string) error {
	return f.current().server.CloseRTPServer(ctx, app, stream)
}
func (f *failoverMedia) ListRTPServers(ctx context.Context) ([]string, error) {
	return f.current().server.ListRTPServers(ctx)
}
