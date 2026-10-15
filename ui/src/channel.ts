import {Socket} from 'node:net';
import {TextDecoder} from 'node:util';
import {invalid, reply} from './schema.js';

export function parseFrame(text: string): unknown {
  const value: unknown = JSON.parse(text);
  // JSON.parse checks syntax; this token walk additionally rejects duplicate object keys.
  const tokens = text.match(/"(?:[^"\\]|\\.)*"|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?|true|false|null|[{}\[\]:,]/g) ?? [];
  let index = 0;
  function visit(depth: number) {
    if (depth > 64) invalid();
    const token = tokens[index++];
    if (token === '{') {
      const keys = new Set<string>();
      while (tokens[index] !== '}') {
        const key: unknown = JSON.parse(tokens[index++] ?? 'null');
        if (typeof key !== 'string' || keys.has(key) || tokens[index++] !== ':') invalid();
        keys.add(key);
        visit(depth + 1);
        if (tokens[index] !== ',') break;
        index++;
      }
      if (tokens[index++] !== '}') invalid();
    } else if (token === '[') {
      while (tokens[index] !== ']') {
        visit(depth + 1);
        if (tokens[index] !== ',') break;
        index++;
      }
      if (tokens[index++] !== ']') invalid();
    } else if (token === undefined) invalid();
  }
  visit(0);
  if (index !== tokens.length) invalid();
  return value;
}

export class Channel {
  // Inherited pipes need cancellable event driven I/O, not a filesystem read blocking a worker.
  private readonly input = new Socket({fd: 4, readable: true, writable: false});
  private readonly output = new Socket({fd: 3, readable: false, writable: true});
  private buffer = Buffer.alloc(0);
  private sequence = 0n;
  private pending: {id: string; resolve: (v: unknown) => void; reject: (e: Error) => void; timer: NodeJS.Timeout} | null = null;
  connected = true;
  onFailure: () => void = () => {};
  constructor() {
    this.input.on('data', chunk => {
      try {
        if (!Buffer.isBuffer(chunk)) invalid();
        this.buffer = Buffer.concat([this.buffer, chunk]);
        if (this.buffer.length > 1048576) invalid();
        const end = this.buffer.indexOf(10);
        if (end < 0) return;
        if (end !== this.buffer.length - 1 || !this.pending) invalid();
        const text = new TextDecoder('utf-8', {fatal: true}).decode(this.buffer.subarray(0, end));
        const value = parseFrame(text);
        const result = reply(value, this.pending.id);
        this.buffer = Buffer.alloc(0);
        const pending = this.pending;
        this.pending = null;
        clearTimeout(pending.timer);
        pending.resolve(result);
      } catch { this.fail(); }
    });
    this.input.on('end', () => this.fail());
    this.input.on('error', () => this.fail());
    this.output.on('error', () => this.fail());
  }
  request(method: string, params: unknown): Promise<unknown> {
    if (this.pending || !this.connected) return Promise.reject(new Error('ui_protocol_invalid'));
    const id = (++this.sequence).toString();
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => this.fail(), method === 'hello' ? 5000 : 12000);
      this.pending = {id, resolve, reject, timer};
      const frame = JSON.stringify({version: 1, id, method, params}) + '\n';
      if (Buffer.byteLength(frame) > 4096) { this.fail(); return; }
      this.output.write(frame, err => { if (err) this.fail(); });
    });
  }
  close() {
    this.connected = false;
    if (this.pending) { clearTimeout(this.pending.timer); this.pending.reject(new Error('ui_closed')); this.pending = null; }
    this.input.destroy();
    this.output.destroy();
  }
  private fail() {
    if (!this.connected) return;
    this.close();
    this.onFailure();
  }
}
