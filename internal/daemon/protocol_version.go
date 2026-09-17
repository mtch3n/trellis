package daemon

// ProtocolVersion changes whenever a method or message changes shape or
// name. Version 2 is the vocabulary rename, and no release carries part of
// it: the liveness method health became ping, and search results took the
// glossary's names (kind entry, unverified).
const ProtocolVersion = 2
