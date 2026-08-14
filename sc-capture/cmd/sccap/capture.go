package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/annotate"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/capture"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/client"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/coverage"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/decode"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/doctor"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/exitcode"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/flow"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/index"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/journal"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/session"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/status"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/version"
	"github.com/anthony-hopkins/star-konflict/sc-capture/pkg/scproto"
	"github.com/gopacket/gopacket"
)

// recordCoverage folds a session's observations into the machine-wide store and
// leaves a delta inside the bundle.
//
// The delta matters as much as the store: it means coverage can be rebuilt from
// bundles alone if the store is lost, and a bundle carries a record of what it
// contributed.
func recordCoverage(bundle *session.Bundle, meta *session.Metadata, stats decode.Stats, rep *status.Reporter) {
	doc, err := coverage.Load(session.DataDir())
	if err != nil {
		rep.Notef("coverage store unavailable: %v", err)
		return
	}

	now := time.Now().UTC()
	var observed []coverage.Entry
	for key := range stats.ObservedElements {
		kind, id, ok := coverage.ParseKey(key)
		if !ok {
			continue
		}
		decoded := strings.HasSuffix(key, "!")
		doc.Observe(kind, id, decoded, bundle.ID, now)

		name, _ := scproto.Name(kind, id)
		state := coverage.ObservedUndecoded
		if decoded {
			state = coverage.Decoded
		}
		observed = append(observed, coverage.Entry{
			Kind: string(kind), ID: id, Name: name, State: state,
			FirstSeenSession: bundle.ID, FirstSeenUTC: now, Observations: 1,
		})
	}

	var novel []coverage.Entry
	for _, e := range stats.Novel {
		doc.Novelty(e.Kind, e.ID, e.Name, bundle.ID, now)
		novel = append(novel, coverage.Entry{
			Kind: string(e.Kind), ID: e.ID, Name: e.Name,
			State: coverage.ObservedUndecoded, FirstSeenSession: bundle.ID,
			FirstSeenUTC: now, Observations: 1,
		})
	}

	if err := coverage.WriteDelta(bundle.Dir, bundle.ID, observed, novel); err != nil {
		rep.Notef("writing %s: %v", coverage.DeltaFile, err)
	}
	if err := doc.Save(); err != nil {
		rep.Notef("saving coverage: %v", err)
	}
	if len(novel) > 0 {
		meta.Anomaly("%d protocol element(s) observed that are unknown to this build", len(novel))
	}
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

// frame is one captured frame in flight between a reader and the writer.
// Data is a copy: the ring buffer slice is only valid until the next read.
type frame struct {
	ifaceID int
	ci      gopacket.CaptureInfo
	data    []byte
	// Filled in by the journal writer once the frame is durable, so a derived
	// record can cite exactly where its evidence lives.
	segment string
	index   uint64
}

func runCapture(args []string) int {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	scenario := fs.String("scenario", session.DefaultScenario, "scenario id for the bundle name")
	volunteer := fs.String("volunteer", session.DefaultVolunteer, "volunteer id")
	region := fs.String("region", "", "server region: EU, NA, RU, SEA")
	out := fs.String("out", "./captures", "parent directory for the bundle")
	minFree := fs.String("min-free", "2GiB", "warn when free space drops below this")
	floor := fs.String("floor", "512MiB", "stop capturing cleanly at this much free space")
	snaplen := fs.Int("snaplen", 0, "bytes captured per frame (0 = whole frame)")
	filter := fs.String("filter", "", "capture-time BPF filter (default none; see warning)")
	relay := fs.Bool("relay", false, "enable the interposition overlay (not yet implemented)")
	clientDir := fs.String("client-dir", "", "game install directory (default: autodetect from Steam)")
	appID := fs.String("app-id", client.StarConflictAppID, "Steam application id of the game")
	noClientHash := fs.Bool("no-client-hash", false, "skip hashing the client binary")
	var ifaces stringList
	fs.Var(&ifaces, "interface", "interface to capture (repeatable; default: autodetect)")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}

	if *relay {
		errf("--relay is not implemented yet; capture is passive. " +
			"Passive capture already archives in-match traffic.")
		return exitcode.Usage
	}
	if *filter != "" {
		errf("--filter is not implemented: a capture-time filter is a permanent, " +
			"irreversible discard decision and this build does not offer one.")
		return exitcode.Usage
	}

	minFreeBytes, err := session.ParseSize(*minFree)
	if err != nil {
		errf("--min-free: %v", err)
		return exitcode.Usage
	}
	floorBytes, err := session.ParseSize(*floor)
	if err != nil {
		errf("--floor: %v", err)
		return exitcode.Usage
	}
	if floorBytes >= minFreeBytes {
		errf("--floor (%s) must be below --min-free (%s)", *floor, *minFree)
		return exitcode.Usage
	}

	// Fail fast on a host that cannot capture, rather than after the
	// contributor has played for an hour.
	rep := doctor.Run(strings.Join(ifaces, ""), session.DataDir(), version.TablesRevision)
	if rep.Failed() {
		errf("this machine cannot capture; run 'sccap doctor' for details")
		for _, c := range rep.Checks {
			if c.Severity == doctor.Fail {
				fmt.Fprintf(os.Stderr, "  %s: %s\n", c.Name, c.Detail)
				if c.Remedy != "" {
					fmt.Fprintf(os.Stderr, "  fix: %s\n", c.Remedy)
				}
			}
		}
		return exitcode.NoCapability
	}

	selected, err := resolveInterfaces(ifaces)
	if err != nil {
		errf("%v", err)
		return exitcode.Usage
	}

	// Identify the client before capture starts, so an abruptly killed session
	// still says which build produced it. Never fatal: an unidentified
	// recording is worth far more than no recording.
	clientInfo := client.Detect(client.Options{
		AppID:      *appID,
		InstallDir: *clientDir,
		HashBinary: !*noClientHash,
	})

	return captureSession(captureOpts{
		scenario:  *scenario,
		volunteer: *volunteer,
		region:    *region,
		out:       *out,
		snaplen:   *snaplen,
		minFree:   minFreeBytes,
		floor:     floorBytes,
		ifaces:    selected,
		client:    clientInfo,
	})
}

// resolveInterfaces picks what to capture.
//
// When several interfaces are live and none was named, this refuses rather than
// guessing. Guessing wrong produces a session that passes every other check and
// contains none of the game's traffic — silent and total. A refusal costs
// thirty seconds; a wrong guess costs the session.
func resolveInterfaces(requested []string) ([]doctor.InterfaceInfo, error) {
	all, err := doctor.Interfaces()
	if err != nil {
		return nil, err
	}
	byName := map[string]doctor.InterfaceInfo{}
	for _, i := range all {
		byName[i.Name] = i
	}

	if len(requested) > 0 {
		var out []doctor.InterfaceInfo
		for _, name := range requested {
			i, ok := byName[name]
			if !ok {
				return nil, fmt.Errorf("no such interface: %s", name)
			}
			if !i.Up {
				return nil, fmt.Errorf("interface %s is down", name)
			}
			out = append(out, i)
		}
		return out, nil
	}

	var live []doctor.InterfaceInfo
	for _, i := range all {
		if i.Up && i.Carrier && !i.Loopback {
			live = append(live, i)
		}
	}
	switch len(live) {
	case 0:
		return nil, fmt.Errorf("no live interface found; specify one with --interface")
	case 1:
		return live, nil
	default:
		names := make([]string, len(live))
		for i, l := range live {
			names[i] = l.Name
		}
		return nil, fmt.Errorf(
			"several interfaces are live (%s) and capturing the wrong one yields a "+
				"clean-looking but worthless session.\n"+
				"       Find the right one:  sccap doctor --watch 30s\n"+
				"       Then:                sccap capture --interface <name>",
			strings.Join(names, ", "))
	}
}

type captureOpts struct {
	scenario, volunteer, region, out string
	snaplen                          int
	minFree, floor                   uint64
	ifaces                           []doctor.InterfaceInfo
	client                           *client.Info
}

func captureSession(o captureOpts) int {
	clock := session.NewClock()
	start := clock.Start()

	bundle, err := session.CreateBundle(o.out, start, o.scenario, o.volunteer, o.region)
	if err != nil {
		errf("%v", err)
		return exitcode.Usage
	}

	meta := session.NewMetadata(bundle.Dir, bundle.ID, session.Software{
		Name:                  "sccap",
		Version:               version.Version,
		GitCommit:             version.Commit,
		ProtocolTablesVersion: version.TablesRevision,
	}, start)
	meta.ScenarioID = o.scenario
	meta.VolunteerID = o.volunteer
	meta.Region = o.region
	meta.Host.CaptureTool = version.UserAgent()
	meta.Host.Snaplen = o.snaplen
	meta.Host.Platform = runtime.GOOS + "/" + runtime.GOARCH
	meta.Clock.Method = "CLOCK_REALTIME + CLOCK_MONOTONIC anchors"
	meta.Client = o.client
	if o.client == nil {
		// Say so in the session rather than leaving a reader to wonder whether
		// nobody looked or nothing was found.
		meta.Anomaly("game client could not be identified; no Steam app manifest was " +
			"found. Record the build by hand in notes.md — a recording without its " +
			"matching client build is frequently undecodable.")
	}

	specs := make([]journal.InterfaceSpec, 0, len(o.ifaces))
	for id, i := range o.ifaces {
		role := "game-uplink"
		if i.Loopback {
			role = "loopback-relay-leg"
		}
		specs = append(specs, journal.InterfaceSpec{
			Name: i.Name, Description: role, SnapLength: uint32(o.snaplen),
		})
		meta.Host.Interfaces = append(meta.Host.Interfaces, session.InterfaceRecord{
			Name: i.Name, Role: role, PcapngID: id, Offloads: i.Offloads,
		})
		if i.Offloads != "" {
			meta.Anomaly("interface %s had offloads enabled (%s): captured frame "+
				"boundaries are the driver's, not the wire's", i.Name, i.Offloads)
		}
	}

	// Write the sidecar before a single frame is captured, so that even a
	// session killed one second from now is self-describing.
	if err := meta.Write(); err != nil {
		errf("write %s: %v", session.MetadataFile, err)
		return exitcode.Usage
	}

	jw, err := journal.NewWriter(bundle.Dir, specs, version.UserAgent(),
		runtime.GOOS+" "+runtime.GOARCH)
	if err != nil {
		errf("open journal: %v", err)
		return exitcode.Usage
	}

	beacon, err := annotate.OpenBeacon(bundle.Dir, annotate.DefaultBeaconPort)
	if err != nil {
		errf("open marker log: %v", err)
		return exitcode.Usage
	}
	meta.Markers.BeaconPort = beacon.Port()
	deriver := annotate.NewDeriver(beacon)

	var sources []*capture.Source
	for _, i := range o.ifaces {
		src, err := capture.Open(i.Name, o.snaplen)
		if err != nil {
			for _, s := range sources {
				s.Close()
			}
			errf("%v", err)
			return exitcode.NoCapability
		}
		sources = append(sources, src)
	}

	_ = session.PublishLive(session.Live{
		PID: os.Getpid(), BundleDir: bundle.Dir, BundleID: bundle.ID,
		StartedAt: start, BeaconPort: beacon.Port(),
	})

	counters := &status.Counters{}
	tty := isTerminal(os.Stderr)
	rep := status.New(os.Stderr, counters, tty)

	names := make([]string, len(o.ifaces))
	for i, x := range o.ifaces {
		names[i] = x.Name
	}
	fmt.Fprintf(os.Stderr, "Capturing %s -> %s\n", strings.Join(names, ","), bundle.Dir)
	if o.client != nil {
		fmt.Fprintf(os.Stderr, "Client: %s\n", o.client.Summary())
	} else {
		fmt.Fprintf(os.Stderr, "Client: NOT IDENTIFIED — note the game build in notes.md, "+
			"or pass --client-dir\n")
	}
	fmt.Fprintf(os.Stderr, "Passive: nothing is rewritten, nothing is filtered. Ctrl+C to stop.\n\n")
	rep.Start()

	frames := make(chan frame, 8192)
	stop := make(chan struct{})
	var readers sync.WaitGroup

	for id, src := range sources {
		readers.Add(1)
		go func(id int, src *capture.Source) {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				data, ci, err := src.Read()
				if err != nil {
					// A poll timeout is normal on a quiet link.
					continue
				}
				buf := make([]byte, len(data))
				copy(buf, data)
				select {
				case frames <- frame{ifaceID: id, ci: ci, data: buf}:
				case <-stop:
					return
				}
			}
		}(id, src)
	}

	// Decode runs downstream of the journal on its own goroutine, fed by a
	// bounded channel that DROPS when full. That drop is the mechanism that
	// makes Principle II unconditional: decode can stall, panic or fall
	// arbitrarily far behind and the worst outcome is a short derived index,
	// which `sccap index --rebuild` regenerates from the journal.
	table := flow.NewTable()
	idx, err := index.Create(bundle.Dir)
	if err != nil {
		errf("open record index: %v", err)
		return exitcode.Usage
	}
	pipeline := decode.New(table, func(r decode.Record) {
		if err := idx.Write(r); err == nil {
			counters.Records.Add(1)
		}
	})
	toDecode := make(chan frame, 4096)
	decodeDone := make(chan struct{})
	go func() {
		defer close(decodeDone)
		for f := range toDecode {
			pipeline.Feed(decode.FrameRef{Segment: f.segment, Index: f.index},
				f.data, f.ci.Timestamp, clock.Mono())
		}
	}()

	// Single writer: the journal is the one place ordering matters, and one
	// goroutine owning it removes an entire class of race.
	writerDone := make(chan struct{})
	var writeErr error
	go func() {
		defer close(writerDone)
		for f := range frames {
			seg, fidx, err := jw.Write(f.ifaceID, f.ci, f.data)
			if err != nil {
				writeErr = err
				return
			}
			counters.Frames.Add(1)
			counters.Bytes.Add(uint64(len(f.data)))

			// Only after the bytes are journaled. Never blocking.
			f.segment, f.index = seg, fidx
			select {
			case toDecode <- f:
			default:
				idx.Dropped()
			}
		}
	}()

	// Ctrl+C is how a session ends normally and is what the manual tells a
	// contributor to press; Go surfaces it, and Ctrl+Break, as os.Interrupt.
	// SIGTERM is listed for a supervisor that emulates it and costs nothing.
	//
	// Closing the console window is deliberately NOT handled, because it
	// cannot be: Windows gives the process a couple of seconds and then kills
	// it outright. That is the interrupted-session path, and it is built to
	// produce a valid bundle rather than a clean one — see
	// tests/e2e/abrupt_test.go.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)

	_, mono := clock.Now()
	_, _ = beacon.Stamp(annotate.KindEvent, "capture started", start, mono)

	termination := session.TerminationClean
	exit := exitcode.OK
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	disk := session.NewDiskMonitor(bundle.Dir, o.minFree, o.floor)
	warnedDisk := false
	lastMetaWrite := time.Now()

loop:
	for {
		select {
		case <-sigs:
			rep.Notef("\nStopping — closing the session cleanly.")
			break loop

		case <-ticker.C:
			if writeErr != nil {
				rep.Notef("journal write failed: %v", writeErr)
				termination = session.TerminationError
				exit = exitcode.Usage
				break loop
			}

			if clock.Tick() {
				rep.Notef("WARNING: the system clock stepped. Monotonic ordering is " +
					"unaffected and the step is recorded in session.json.")
				meta.Anomaly("wall-clock step detected during capture")
			}
			_ = jw.Tick()
			_ = idx.Tick()

			// Surface discoveries as they happen: a novel element is the most
			// interesting thing that can occur during a capture, and a
			// contributor who sees one appear can go and exercise more of
			// whatever they just did.
			st := pipeline.Stats()
			if uint64(len(st.Novel)) > counters.Novel.Load() {
				for _, e := range st.Novel[counters.Novel.Load():] {
					rep.Notef("NOVEL: %s id %d — not in this build's element tables. "+
						"Whatever you just did is worth repeating.", e.Kind, e.ID)
				}
				counters.Novel.Store(uint64(len(st.Novel)))
			}
			// Annotate the timeline from the traffic itself, so the session is
			// interpretable without the contributor having narrated anything.
			for _, m := range deriver.Observe(st, time.Now().UTC(), clock.Mono()) {
				rep.Notef("mark: %s", m.Label)
			}
			for _, svc := range table.Services() {
				rep.Service(svc)
			}

			var packets, drops uint64
			for _, s := range sources {
				p, d, err := s.Stats()
				if err == nil {
					packets += p
					drops += d
				}
			}
			counters.Drops.Store(drops)
			meta.Host.PacketsCaptured = packets
			meta.Host.PacketsDropped = drops

			if drops > 0 && !warnedDisk {
				rep.Notef("WARNING: the kernel has dropped %d packets. This session is "+
					"missing traffic that crossed the wire.", drops)
			}

			state, free, err := disk.Check()
			if err == nil {
				switch state {
				case session.DiskWarn:
					if !warnedDisk {
						rep.Notef("WARNING: only %s free. Capture will stop cleanly at %s.",
							session.HumanSize(free), session.HumanSize(o.floor))
						warnedDisk = true
					}
				case session.DiskFloor:
					rep.Notef("Free space reached the floor (%s). Closing this session "+
						"cleanly. No previous session has been touched.",
						session.HumanSize(free))
					meta.Anomaly("capture stopped at the disk floor with %s free",
						session.HumanSize(free))
					termination = session.TerminationDiskFloor
					exit = exitcode.DiskFloor
					break loop
				}
			}

			// Periodic sidecar refresh, so an abrupt kill leaves recent
			// metadata rather than only what was known at second zero.
			if time.Since(lastMetaWrite) >= session.AnchorInterval {
				meta.Clock.Anchors = clock.Anchors()
				meta.Segments = toSegmentRecords(jw.Segments())
				_ = meta.Write()
				lastMetaWrite = time.Now()
			}
		}
	}

	// Shutdown, in the order that keeps the journal consistent: stop reading,
	// drain the journal, then let decode finish what it already holds.
	close(stop)
	readers.Wait()
	close(frames)
	<-writerDone
	close(toDecode)
	<-decodeDone

	stats := pipeline.Stats()
	if err := idx.Close(); err != nil {
		errf("closing record index: %v", err)
	}
	_, droppedRecords := idx.Counts()
	if droppedRecords > 0 {
		// Say so rather than let a short index read as "nothing else happened".
		meta.Anomaly("%d frames were not decoded live because decoding fell behind; "+
			"the journal is complete and 'sccap index --rebuild' will recover them",
			droppedRecords)
	}

	meta.CredentialWarning = stats.CredentialsSeen
	meta.Connections = table.Connections()
	meta.ServicesObserved = table.Services()
	for _, h := range stats.Handoffs {
		meta.Anomaly("dedicated-server handoff observed: %s:%d zone %d (session %d)",
			h.Addr, h.Port, h.ZoneID, h.SessionID)
	}
	recordCoverage(bundle, meta, stats, rep)

	endMono := clock.Mono()
	_, _ = beacon.Stamp(annotate.KindEvent, "capture stopped", time.Now().UTC(), endMono)
	meta.Markers.Count = beacon.Count()
	meta.Markers.LastSeq = beacon.Count()
	if meta.Markers.Count > 0 {
		meta.Markers.FirstSeq = 1
	}
	_ = beacon.Close()

	for _, s := range sources {
		p, d, err := s.Stats()
		if err == nil {
			meta.Host.PacketsCaptured += p
			meta.Host.PacketsDropped += d
		}
		s.Close()
	}

	if err := jw.Close(); err != nil {
		errf("closing journal: %v", err)
		termination = session.TerminationError
		exit = exitcode.Usage
	}

	clock.Close()
	end := time.Now().UTC()
	meta.UTCEnd = &end
	meta.Clock.Anchors = clock.Anchors()
	meta.Segments = toSegmentRecords(jw.Segments())
	meta.Termination = termination
	if err := meta.Write(); err != nil {
		errf("writing %s: %v", session.MetadataFile, err)
		exit = exitcode.Usage
	}

	// SHA256SUMS last, after everything else is on disk. Its presence is
	// itself the evidence that this session closed cleanly.
	if err := journal.WriteSums(bundle.Dir); err != nil {
		errf("writing %s: %v", journal.SumsFile, err)
		exit = exitcode.Usage
	}

	_ = session.ClearLive()
	rep.Stop()

	frameCount, byteCount := jw.Stats()
	fmt.Fprintf(os.Stderr, "\nSession: %s\n", bundle.Dir)
	fmt.Fprintf(os.Stderr, "  %d frames, %s journaled, %d dropped by the kernel\n",
		frameCount, session.HumanSize(byteCount), meta.Host.PacketsDropped)
	fmt.Fprintf(os.Stderr, "  Marked SENSITIVE: this session may contain login credentials in the clear.\n")
	fmt.Fprintf(os.Stderr, "  Nothing has been sent anywhere. Verify before sharing:\n")
	fmt.Fprintf(os.Stderr, "    sccap verify %s\n", bundle.Dir)
	return exit
}

func toSegmentRecords(in []journal.SegmentInfo) []session.SegmentRecord {
	out := make([]session.SegmentRecord, 0, len(in))
	for _, s := range in {
		out = append(out, session.SegmentRecord{
			File: s.File, FirstFrameAt: s.FirstFrameAt, LastFrameAt: s.LastFrameAt, Frames: s.Frames,
		})
	}
	return out
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
