// Package uow provides a lightweight, database-agnostic Unit of Work pattern.
//
// The [UnitOfWork] interface orchestrates begin → execute → commit/rollback lifecycles.
// Transactions are created via [TxFactory] and represented by [Tx].
package uow
