# Verify bundle digests with inspect

`quo inspect` checks bundle members against the digests in the manifest
without contacting a cluster. Neither `quo deploy` nor its dry run checks
member digests. Hashing a disk during upload checks the local bundle bytes,
not what the target receives.
