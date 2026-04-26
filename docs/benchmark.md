## Performance

Benchmark results comparing WinMcpShim against two popular MCP file servers
(median of 20 calls per operation, Windows 10):

| Operation | WinMcpShim | MCP Filesystem | Desktop Commander |
|-----------|-----------|----------------|-------------------|
| Cold start | 40 ms | 308 ms (7.8×) | 1606 ms (41×) |
| Read 1 KB | 518 µs | 1.1 ms (2.0×) | 39.9 ms (77×) |
| Read 100 KB | 2.0 ms | 3.0 ms (1.5×) | 40.1 ms (20×) |
| List directory | 999 µs | 1.0 ms (1.0×) | 20.8 ms (21×) |
| File info | 1.0 ms | 530 µs (0.5×) | 20.4 ms (20×) |
| Write file | 1.4 ms | 2.0 ms (1.4×) | 38.6 ms (28×) |
| Throughput | 232 µs/call | 728 µs/call (3.1×) | 38.9 ms/call (168×) |
| Grep (200 files) | 26.1 ms | — | — |

Grep is always served by the built-in Go RE2 implementation. External
GNU grep (Git for Windows MSYS2 binary) is forbidden because it
mangles Windows paths under recursive search.

Reproduce with `mage bench` (requires all three servers installed).
