# TODO

## 2026-09-17
- [ ] Detect Redis disconnection/downtime - currently no reconnect or fallback logic if Redis goes down while the server is running

## 2026-09-21
- [ ] Make testcontainers instances independent of each other, it does make test suite slower to run on overall
    - This is still a maybe, well see if it is needed
- [ ] Set period duration to milliseconds (millis will support additional algorithms too, currently only fixed window is supported), currently minutes only are supported
- [ ] Make a test with two handlers, so they'll compete for for the same key -- priority

# 30-10-2026
- [✓] Pin down Redis version -- priority
- [ ] Change: redis.NewScript runs on every call, and gets hashed each time. Build each script once as a package-level variable, and have the factory return those.
- [ ] Add: sliding window algo
- [ ] Make: the rules configurable through files, such as: {name, algorithm, limit, period, key_by}, and load at startup

# 02-11-2026
- [ ] Change: func ratelimitAlgorithmFactory to return pre-compiled (not sure if this is the right term) objects, instead of doing it every time
