## 2024-05-24 - Information Disclosure via raw error messages
**Vulnerability:** The application was exposing raw error messages (`err.Error()`) to clients when an internal error occurred in `handleError`. This could potentially leak sensitive information like database queries, internal file paths, or stack traces.
**Learning:** Always provide generic error messages for 500 status codes while logging the actual error internally for debugging.
**Prevention:** Ensure that global error handlers sanitize the messages returned to the client and only expose detailed messages if they are explicitly intended for the client (e.g., `fiber.Error`).
