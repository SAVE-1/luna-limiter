# Performance
## Load Testing, best case
The rate limiter was load tested using k6 with a constant arrival rate of 12,000 requests per second sustained for 60 seconds on a single node.

The test was executed in WSL, with Redis and the rate-limiter service running on the same network, on an Intel i5-12600K.

Test characteristics:
- Traffic pattern: constant arrival rate
- Client cardinality: high (unique client per request)
- Endpoint: POST /v1/ratelimit

Results, every user is unique:
- Sustained throughput: ~12k requests/sec
- avg latency: ~1.68 ms
- med latency: ~0.58 ms
- p95 latency: ~5.39 ms
- p99 latency: ~19 ms

Results, 100 unique users:
- Sustained throughput: ~12k requests/sec
- avg latency: ~1.22 ms
- med latency: ~0.23 ms
- p95 latency: ~1.07 ms
- p99 latency: ~35 ms

These benchmarks are intended to explore latency behavior and throughput limits under controlled conditions, rather than to claim production-level performance. 
Running Redis and the service on a single node minimizes network overhead and highlights the cost of synchronization and Lua execution.
The results helped identify concurrency limits and informed how the design would need to evolve for multi-node deployments (e.g. sharding, local caching, or alternative coordination strategies).