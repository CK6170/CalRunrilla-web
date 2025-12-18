/**
 * POST JSON to an API endpoint and return the decoded JSON response.
 *
 * Conventions:
 * - Sends `Content-Type: application/json`
 * - Always attempts to parse JSON (falls back to `{}` if parsing fails)
 * - Throws an Error for non-2xx responses using `data.error` when present
 *
 * @param {string} url - API endpoint (ex: `/api/connect`).
 * @param {any} [body] - Request payload (will be JSON.stringified; defaults to `{}`).
 * @returns {Promise<any>} Parsed JSON response body.
 * @throws {Error} When the response is not OK (includes server-provided `error` when available).
 */
export async function apiJSON(url, body) {
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body ?? {}),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `${res.status} ${res.statusText}`);
  return data;
}

/**
 * Upload a file via multipart/form-data to an API endpoint and return decoded JSON.
 *
 * The server expects the file field name to be `file`.
 *
 * @param {string} url - Upload endpoint (ex: `/api/upload/config`).
 * @param {File|Blob} file - The file/blob to upload.
 * @returns {Promise<any>} Parsed JSON response body.
 * @throws {Error} When the response is not OK (includes server-provided `error` when available).
 */
export async function uploadFile(url, file) {
  const fd = new FormData();
  fd.append("file", file);
  const res = await fetch(url, { method: "POST", body: fd });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `${res.status} ${res.statusText}`);
  return data;
}


