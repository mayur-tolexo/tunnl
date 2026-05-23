# tunnl
A standalone Go module producing two binaries + one shared package: - tunnld — the relay. Runs on your public VPS, holds the wildcard cert, listens on :443 (and :80 → :443 redirect). - tunnl — the client CLI. Runs on the user's machine: tunnl http 3000. - protocol — shared package: control message types + framing, imported by both.
