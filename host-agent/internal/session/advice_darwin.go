package session

// localNetworkAdvice is the line, after a connection failed, about the
// permission macOS (15 and newer) keeps for reaching computers on the same
// network: without it a viewer next door cannot be reached.
const localNetworkAdvice = "If the other computer is on this network: System Settings > Privacy & Security > Local Network, and allow FreeDesk there."
