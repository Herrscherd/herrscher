# Architecture

Hexagonal.
[herrscher-contracts](https://github.com/Herrscherd/herrscher-contracts) is the
authority: it declares the ports and the neutral types, with zero platform
mechanics. Every dependency arrow points inward at it. The agnostic domain
(`core`) depends on no edge, the edges depend on no core, and only the host's
wiring file ever sees two concrete types at once.

Purity tests (`TestHostPurity`, `TestCorePurity`,
`TestCoreNamesNoConcretePlatform`) fail the build if a concrete platform leaks
in.



## A note on history

The platform grew out of a Go monolith that bridged Discord to a local Claude.
Herrscher is that monolith decomposed along its natural seams (channel, model,
domain) so each can evolve independently. The contract shapes were chosen
deliberately to make the eventual transport change, in-process to NATS/gRPC, a
detail rather than a rewrite.

