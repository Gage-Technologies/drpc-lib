# drpc

This repo contains utilities for the [DRPC](https://github.com/storj/drpc) framework.

Currently, it contains the following utilities:

- [muxconn](muxconn): Drop-in replacement for drpcconn with stream multiplexing support via yamux.
- [muxserver](muxserver): Drop-in replacement for drpcserver with stream multiplexing support via yamux.