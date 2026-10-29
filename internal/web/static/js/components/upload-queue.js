// Reusable upload queue: a drop zone, a list with progress bars,
// and send/clear buttons. Used by the game and music views.

import { esc, fmtBytes, plural } from "../format.js";
import { uploadFile } from "../api.js";

let counter = 0;

/**
 * Builds the queue inside `root`.
 *
 * Options:
 *  - accept:   value of the file input accept attribute (optional)
 *  - hint:     text below "Drop files here"
 *  - validate: (file) => error message, or null when the file can be sent
 *  - canSend:  () => true when the view's choices allow sending
 *  - url:      () => API path for the upload (read when Send is clicked)
 *  - onStart:  called before the first upload (e.g. to lock form fields)
 *  - onFinish: ({ sent, failed }) => called when the queue is done
 *
 * Returns { revalidate }, so the view can ask for a new check when its
 * choices change (e.g. another system was selected).
 */
export function createUploadQueue(root, {
  accept = "",
  hint = "or click to choose. You can send several at once.",
  validate = () => null,
  canSend = () => true,
  url,
  onStart = () => {},
  onFinish = () => {},
}) {
  const id = `uq-files-${++counter}`;
  root.innerHTML = `
    <label class="drop" for="${id}">
      <b>Drop files here</b>
      <span class="muted">${esc(hint)}</span>
      <input type="file" id="${id}" multiple class="sr-only" ${accept ? `accept="${esc(accept)}"` : ""}>
    </label>
    <div class="uq-controls">
      <span class="muted uq-count">No files selected.</span>
      <div class="actions">
        <button class="btn uq-clear" disabled>Clear list</button>
        <button class="btn btn-primary uq-send" disabled>Upload</button>
      </div>
    </div>
    <div class="queue"></div>`;

  const drop = root.querySelector(".drop");
  const input = root.querySelector("input[type=file]");
  const count = root.querySelector(".uq-count");
  const clear = root.querySelector(".uq-clear");
  const send = root.querySelector(".uq-send");
  const list = root.querySelector(".queue");
  const queue = [];
  let sending = false;

  function revalidate() {
    for (const q of queue) {
      if (q.state === "done" || q.state === "sending") continue;
      const problem = validate(q.file);
      if (problem) { q.state = "invalid"; q.msg = problem; }
      else { q.state = "ready"; q.msg = "Ready to upload"; }
    }
    draw();
  }

  function addFiles(files) {
    for (const f of files) {
      if (queue.some(q => q.file.name === f.name && q.file.size === f.size)) continue;
      queue.push({ file: f, state: "ready", msg: "", progress: 0 });
    }
    revalidate();
  }

  function draw() {
    const pending = queue.filter(q => q.state === "ready").length;
    count.textContent = queue.length
      ? `${plural(queue.length, "file", "files")}, ${pending} ready to upload.`
      : "No files selected.";
    send.disabled = sending || !pending || !canSend();
    send.textContent = pending ? `Upload ${plural(pending, "file", "files")}` : "Upload";
    clear.disabled = sending || !queue.length;
    list.innerHTML = queue.map(q => {
      const st = q.state === "invalid" || q.state === "error" ? "error" : q.state;
      return `<div class="qitem" data-state="${st}">
        <span class="qname">${esc(q.file.name)} <span class="muted small num">${fmtBytes(q.file.size)}</span></span>
        <span class="qstatus">${esc(q.msg)}</span>
        <div class="bar"><i style="width:${Math.round(q.progress * 100)}%"></i></div>
      </div>`;
    }).join("");
  }

  input.onchange = () => { addFiles(input.files); input.value = ""; };
  drop.ondragover = e => { e.preventDefault(); drop.classList.add("over"); };
  drop.ondragleave = () => drop.classList.remove("over");
  drop.ondrop = e => { e.preventDefault(); drop.classList.remove("over"); addFiles(e.dataTransfer.files); };
  clear.onclick = () => { queue.length = 0; draw(); };

  send.onclick = async () => {
    const path = url();
    sending = true;
    onStart();
    let sent = 0, failed = 0;
    for (const q of queue) {
      if (q.state !== "ready") continue;
      q.state = "sending"; q.msg = "Uploading…"; q.progress = 0;
      draw();
      try {
        await uploadFile(path, q.file, p => {
          q.progress = p;
          q.msg = p < 1 ? `Uploading ${Math.round(p * 100)}%` : "Checking on the console…";
          draw();
        });
        q.state = "done"; q.msg = "Uploaded"; q.progress = 1; sent++;
      } catch (err) {
        q.state = "error"; q.msg = err.message; failed++;
      }
      draw();
    }
    sending = false;
    draw();
    onFinish({ sent, failed });
  };

  draw();
  return { revalidate };
}
