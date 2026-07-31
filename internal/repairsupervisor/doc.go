// Package repairsupervisor implements the dedicated supervisor FULL-sync
// journal and at-most-once claim/recovery matrix for the P6 controlled-repair
// runtime.
//
// The journal uses SQLite with FULL journal mode and fullfsync to ensure
// crash-safe durability of phase claims. Three unique claims are recorded:
// materialization_claim (seq 1), adapter_claim (seq 2), test_claim (seq 3).
// Each claim binds authorization, policy, P4 evidence, fence, request,
// dispatch, and predecessor identity. At-most-once semantics ensure duplicate
// callers perform zero effects and prior-epoch nonterminal claims become
// waiting_for_human.
package repairsupervisor
