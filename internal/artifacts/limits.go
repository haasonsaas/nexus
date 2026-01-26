package artifacts

// MaxInlineDataBytes is the maximum size (in bytes) for returning artifact data inline.
// This aligns with the proto comment for Artifact.data ("< 1MB").
const MaxInlineDataBytes int64 = 1024 * 1024
