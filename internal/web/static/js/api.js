// BatoX API client. Every call goes through here.

import { state } from "./state.js";
import { setConn } from "./ui.js";

export class ApiError extends Error {
  constructor(status, message) { super(message); this.status = status; }
}

function authHeaders() {
  return state.token ? { Authorization: "Bearer " + state.token } : {};
}

export async function api(path, { method = "GET", body } = {}) {
  const headers = authHeaders();
  let payload;
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  let res;
  try {
    res = await fetch(state.base + "/api/v1" + path, { method, headers, body: payload });
  } catch {
    setConn(false, "Offline");
    throw new ApiError(0, `The API did not respond at ${state.base}. Check that the service is running and that the address on the Access screen is right.`);
  }
  const text = await res.text();
  let data = null;
  if (text) { try { data = JSON.parse(text); } catch { data = text; } }
  if (!res.ok) {
    const msg = (data && data.error) || `The API returned error ${res.status}.`;
    if (res.status === 401) setConn(false, "Invalid token");
    throw new ApiError(res.status, msg);
  }
  setConn(true, "Connected");
  return data;
}

// Downloads an authenticated file (e.g. a music preview). <audio> cannot send
// the token by itself, so the page downloads the file and plays a local copy.
export async function fetchBlob(path) {
  let res;
  try {
    res = await fetch(state.base + "/api/v1" + path, { headers: authHeaders() });
  } catch {
    throw new ApiError(0, `The API did not respond at ${state.base}.`);
  }
  if (!res.ok) {
    let msg = "";
    try { msg = (await res.json()).error; } catch { msg = ""; }
    throw new ApiError(res.status, msg || `The API returned error ${res.status}.`);
  }
  return res.blob();
}

// Upload with progress (fetch doesn't report upload progress yet).
export function uploadFile(path, file, onProgress) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", state.base + "/api/v1" + path);
    for (const [k, v] of Object.entries(authHeaders())) xhr.setRequestHeader(k, v);
    xhr.upload.onprogress = e => { if (e.lengthComputable) onProgress(e.loaded / e.total); };
    xhr.onload = () => {
      let data = null;
      try { data = JSON.parse(xhr.responseText); } catch { data = null; }
      if (xhr.status >= 200 && xhr.status < 300) resolve(data);
      else reject(new ApiError(xhr.status, (data && data.error) || `The API returned error ${xhr.status}.`));
    };
    xhr.onerror = () => reject(new ApiError(0, "The connection dropped during the upload."));
    const form = new FormData();
    form.append("file", file, file.name);
    xhr.send(form);
  });
}
