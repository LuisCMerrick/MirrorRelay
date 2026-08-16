// Package limit provides concurrency limiters for requests.
package limit

import "sync"

type Limiter struct {
	mu              sync.Mutex
	total           int
	byIP            map[string]int
	byMirror        map[int64]int
	maxTotal, maxIP int
}

func New(maxTotal, maxIP int) *Limiter {
	return &Limiter{byIP: make(map[string]int), byMirror: make(map[int64]int), maxTotal: maxTotal, maxIP: maxIP}
}

func (l *Limiter) Acquire(ip string, mirrorID int64, mirrorMax int) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.maxTotal > 0 && l.total >= l.maxTotal {
		return nil, false
	}
	if l.maxIP > 0 && l.byIP[ip] >= l.maxIP {
		return nil, false
	}
	if mirrorMax > 0 && l.byMirror[mirrorID] >= mirrorMax {
		return nil, false
	}
	l.total++
	l.byIP[ip]++
	l.byMirror[mirrorID]++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.total--
			l.byIP[ip]--
			l.byMirror[mirrorID]--
			if l.byIP[ip] == 0 {
				delete(l.byIP, ip)
			}
			if l.byMirror[mirrorID] == 0 {
				delete(l.byMirror, mirrorID)
			}
		})
	}, true
}
