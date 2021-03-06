package main

import (
	log "code.google.com/p/log4go"
	itime "github.com/Terry-Mao/goim/libs/time"
	"sync"
	"time"
)

type RoomOptions struct {
	ChannelSize int
	ProtoSize   int
	BatchNum    int
	SignalTime  time.Duration
}

type Room struct {
	id      int32
	rLock   sync.RWMutex
	proto   Ring
	signal  chan int
	chs     map[*Channel]struct{} // map room id with channels
	timer   *itime.Timer
	sigTime time.Duration
	options RoomOptions
}

// NewRoom new a room struct, store channel room info.
func NewRoom(id int32, t *itime.Timer, options RoomOptions) (r *Room) {
	r = new(Room)
	r.id = id
	r.options = options
	r.proto.Init(options.ProtoSize)
	r.signal = make(chan int, SignalNum)
	r.chs = make(map[*Channel]struct{}, options.ChannelSize)
	r.timer = t
	go r.push()
	return
}

// Put put channel into the room.
func (r *Room) Put(ch *Channel) {
	r.rLock.Lock()
	r.chs[ch] = struct{}{}
	r.rLock.Unlock()
	return
}

// Del delete channel from the room.
func (r *Room) Del(ch *Channel) {
	r.rLock.Lock()
	delete(r.chs, ch)
	r.rLock.Unlock()
}

// push merge proto and push msgs in batch.
func (r *Room) push() {
	var (
		n     int
		done  bool
		err   error
		p     *Proto
		ch    *Channel
		last  time.Time
		td    *itime.TimerData
		least time.Duration
		vers  = make([]int32, r.options.BatchNum)
		ops   = make([]int32, r.options.BatchNum)
		msgs  = make([][]byte, r.options.BatchNum)
	)
	if Debug {
		log.Debug("start room: %d goroutine", r.id)
	}
	td = r.timer.Add(r.options.SignalTime, func() {
		r.signal <- ProtoReady
	})
	for {
		if n > 0 {
			if least = r.options.SignalTime - time.Now().Sub(last); least > 0 {
				r.timer.Set(td, least)
			} else {
				done = true
			}
		} else {
			last = time.Now()
		}
		if !done {
			if <-r.signal != ProtoReady {
				break
			}
		}
		// merge msgs
		for {
			if n >= r.options.BatchNum {
				// msgs full
				done = true
				break
			}
			if p, err = r.proto.Get(); err != nil {
				// must be empty error
				break
			}
			vers[n] = int32(p.Ver)
			ops[n] = p.Operation
			msgs[n] = p.Body
			n++
			r.proto.GetAdv()
		}
		if !done {
			continue
		}
		if n > 0 {
			r.rLock.RLock()
			for ch, _ = range r.chs {
				// ignore error
				ch.Pushs(vers[:n], ops[:n], msgs[:n])
			}
			r.rLock.RUnlock()
			n = 0
		}
		done = false
	}
	r.timer.Del(td)
	if Debug {
		log.Debug("room: %d goroutine exit", r.id)
	}
}

// Push push msg to the room.
func (r *Room) Push(ver int16, operation int32, msg []byte) (err error) {
	var proto *Proto
	r.rLock.Lock()
	if proto, err = r.proto.Set(); err == nil {
		proto.Ver = ver
		proto.Operation = operation
		proto.Body = msg
		r.proto.SetAdv()
	}
	r.rLock.Unlock()
	select {
	case r.signal <- ProtoReady:
	default:
	}
	return
}

// Online get online number.
func (r *Room) Online() (o int) {
	r.rLock.RLock()
	o = len(r.chs)
	r.rLock.RUnlock()
	return
}

// Close close the room.
func (r *Room) Close() {
	var ch *Channel
	// if chan full, wait
	r.signal <- ProtoFinish
	r.rLock.RLock()
	for ch, _ = range r.chs {
		ch.Close()
	}
	r.rLock.RUnlock()
}
