package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"io"
	"strconv"
	"strings"
	"time"
)

const AuthProtocolVersion = 2

type DeviceType string

const (
	DevicePhone   DeviceType = "phone"
	DeviceTablet  DeviceType = "tablet"
	DeviceDesktop DeviceType = "desktop"
)

func (d DeviceType) valid() bool { return d == DevicePhone || d == DeviceTablet || d == DeviceDesktop }

// ClassifyDevice groups browser information; it never authorizes a hardware identity.
func ClassifyDevice(userAgent string, maxTouchPoints int) DeviceType {
	ua := strings.ToLower(userAgent)
	if strings.Contains(ua, "ipad") || strings.Contains(ua, "tablet") ||
		(strings.Contains(ua, "android") && !strings.Contains(ua, "mobile")) ||
		(strings.Contains(ua, "macintosh") && maxTouchPoints > 1) {
		return DeviceTablet
	}
	if strings.Contains(ua, "iphone") || strings.Contains(ua, "ipod") ||
		(strings.Contains(ua, "android") && strings.Contains(ua, "mobile")) {
		return DevicePhone
	}
	return DeviceDesktop
}

type sessionRef struct {
	UID    int64
	Device DeviceType
	SID    string
}

func randomHex(reader io.Reader, size int) (string, error) {
	buf := make([]byte, size)
	if _, err := io.ReadFull(reader, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
func validHex(s string, size int) bool {
	if len(s) != size*2 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
func newRefreshToken(reader io.Reader, ref sessionRef) (string, error) {
	secret, err := randomHex(reader, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("v2.%d.%s.%s.%s", ref.UID, ref.Device, ref.SID, secret), nil
}
func parseRefresh(token string) (sessionRef, error) {
	p := strings.Split(token, ".")
	if len(p) != 5 || p[0] != "v2" || !validHex(p[3], 16) || !validHex(p[4], 32) {
		return sessionRef{}, ErrRefreshInvalid
	}
	uid, err := strconv.ParseInt(p[1], 10, 64)
	ref := sessionRef{uid, DeviceType(p[2]), p[3]}
	if err != nil || uid <= 0 || !ref.Device.valid() {
		return sessionRef{}, ErrRefreshInvalid
	}
	return ref, nil
}
func refreshDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type authSession struct {
	SID           string `json:"sid"`
	Epoch         int64  `json:"epoch"`
	Digest        string `json:"digest"`
	Previous      string `json:"previous,omitempty"`
	PreviousUntil int64  `json:"previous_until,omitempty"`
	CreatedAt     int64  `json:"created_at"`
	RefreshedAt   int64  `json:"refreshed_at"`
	ExpiresAt     int64  `json:"expires_at"`
}

// One persistent hash bounds account storage to epoch plus three slots.
type authSessionStore struct {
	rdb    *redis.Client
	prefix string
}

func (s authSessionStore) key(uid int64) string { return s.prefix + strconv.FormatInt(uid, 10) }
func authUnavailable(err error) error           { return fmt.Errorf("%w: %v", ErrAuthUnavailable, err) }
func (s authSessionStore) epoch(ctx context.Context, uid int64) (int64, error) {
	ep, err := s.rdb.HGet(ctx, s.key(uid), "epoch").Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, authUnavailable(err)
	}
	return ep, nil
}
func (s authSessionStore) read(ctx context.Context, ref sessionRef, now time.Time) (*authSession, error) {
	values, err := s.rdb.HGetAll(ctx, s.key(ref.UID)).Result()
	if err != nil {
		return nil, authUnavailable(err)
	}
	var record authSession
	raw := values[string(ref.Device)]
	if raw == "" {
		return nil, ErrRefreshInvalid
	}
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return nil, authUnavailable(err)
	}
	ep, err := strconv.ParseInt(values["epoch"], 10, 64)
	if err != nil {
		return nil, authUnavailable(err)
	}
	if record.Epoch != ep || record.ExpiresAt <= now.UnixMilli() {
		return nil, ErrRefreshInvalid
	}
	if record.SID != ref.SID {
		return nil, ErrSessionReplaced
	}
	return &record, nil
}

// Validate all inputs before writing; a single hash avoids dynamic script keys.
var sessionMutation = redis.NewScript(`
local op, device, expected, sid, digest, raw, now, bearer = unpack(ARGV)
local epoch = redis.call('HGET', KEYS[1], 'epoch') or '0'
if not tonumber(epoch) then return redis.error_reply('invalid auth epoch') end
if op == 'login' then
 if epoch ~= expected then return 'changed' end
 local next = cjson.decode(raw)
 if next.sid ~= sid or tostring(next.epoch) ~= epoch then return redis.error_reply('invalid candidate') end
 redis.call('HSET', KEYS[1], 'epoch', epoch, device, raw)
 return 'ok'
end
if op ~= 'revoke' then
 local value = redis.call('HGET', KEYS[1], device)
 if not value then return 'invalid' end
 local current = cjson.decode(value)
 if current.sid ~= sid then return 'replaced' end
 if tostring(current.epoch) ~= epoch or current.expires_at <= tonumber(now) then return 'invalid' end
 if expected ~= '' and expected ~= epoch then return 'invalid' end
 if bearer ~= 'yes' and current.digest ~= digest then
  if op == 'refresh' and current.previous == digest and (current.previous_until or 0) > tonumber(now) then return 'conflict' end
  return 'invalid'
 end
 if op == 'refresh' then
  local next = cjson.decode(raw)
  if next.sid ~= sid or tostring(next.epoch) ~= epoch then return redis.error_reply('invalid candidate') end
  redis.call('HSET', KEYS[1], device, raw)
  return 'ok'
 end
 if op ~= 'logout' then return redis.error_reply('invalid operation') end
end
redis.call('HINCRBY', KEYS[1], 'epoch', 1)
redis.call('HDEL', KEYS[1], 'phone', 'tablet', 'desktop')
return 'ok'
`)

func (s authSessionStore) mutate(ctx context.Context, op string, ref sessionRef, expected, digest string, next *authSession, now time.Time, bearer bool) error {
	raw := ""
	if next != nil {
		encoded, err := json.Marshal(next)
		if err != nil {
			return err
		}
		raw = string(encoded)
	}
	credential := ""
	if bearer {
		credential = "yes"
	}
	result, err := sessionMutation.Run(ctx, s.rdb, []string{s.key(ref.UID)}, op, string(ref.Device), expected, ref.SID, digest, raw, now.UnixMilli(), credential).Text()
	if err != nil {
		return authUnavailable(err)
	}
	switch result {
	case "ok":
		return nil
	case "replaced":
		return ErrSessionReplaced
	case "conflict":
		return ErrRefreshConflict
	case "changed":
		return ErrAuthChanged
	default:
		return ErrRefreshInvalid
	}
}
