# Try state bucket bootstrap

Edit `s3.ub` with a new, unique bucket name and your AWS region.
Edit `gcs.ub` with a new, unique bucket name and your GCP project ID.

Use your normal AWS credentials. For GCS, set up Application Default
Credentials with `gcloud auth application-default login`.

From the repository root, compile once:

```sh
go run ./cmd/unobin compile -p examples/state-bootstrap/factory.ub \
  -o _output/state-bootstrap-example --replace-unobin="$(pwd)" --build
```

Try S3:

```sh
_output/state-bootstrap-example/state-bootstrap plan --allow-version-mismatch \
  -c examples/state-bootstrap/s3.ub -o _output/state-bootstrap-example/s3.ubp
_output/state-bootstrap-example/state-bootstrap apply _output/state-bootstrap-example/s3.ubp
_output/state-bootstrap-example/state-bootstrap output -c examples/state-bootstrap/s3.ub
```

Try GCS:

```sh
_output/state-bootstrap-example/state-bootstrap plan --allow-version-mismatch \
  -c examples/state-bootstrap/gcs.ub -o _output/state-bootstrap-example/gcs.ubp
_output/state-bootstrap-example/state-bootstrap apply _output/state-bootstrap-example/gcs.ubp
_output/state-bootstrap-example/state-bootstrap output -c examples/state-bootstrap/gcs.ub
```

The first `plan` creates the bucket and shows a create for the greeting file.
`apply` writes `_output/state-bootstrap-example/greeting.txt` and saves state
in the bucket. Output shows `message: 'State backend works!'`.
Run `plan` again: it should show `No changes`.

The examples use `noop` state encryption. The buckets remain until you delete
them yourself.

[GCS authentication](https://docs.cloud.google.com/docs/authentication/provide-credentials-adc)
