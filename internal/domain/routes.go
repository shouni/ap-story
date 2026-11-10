package domain

// WorkerTaskPath is the path on which the worker receives tasks.
// Both the enqueuing side (tasks.Config.WorkerPath) and the receiving side (route registration in internal/server) use it.
// With two copies, changing only one would still build and every enqueued task would 404.
// Appending it to the service URL and normalising it is done by gcp-kit's tasks.
const WorkerTaskPath = "/tasks/generate"
