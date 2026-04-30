// Package state hosts state-backend implementations.
//
// Built-in backends (per config.ub state: block):
//   - local: filesystem
//   - s3: S3 with DynamoDB locking
//   - gcs: GCS with generation-based locking
//   - azure-blob: Azure Blob with blob-lease locking
//
// Encryption at rest is mandatory across all backends. Key sources:
//   - env: 32-byte symmetric key in a named env var
//   - kms: KMS-wrapped per-snapshot data key (AWS KMS, GCP KMS, Azure Key Vault)
//
// Locking acquires during apply and refresh (plan is read-only and lock-free).
// Acquisition timeout configurable; force-unlock command exists for stuck locks.
package state
