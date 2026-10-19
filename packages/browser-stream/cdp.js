import WebSocket from 'ws';

// Foxbridge implements Runtime.evaluate, but Runtime.addBinding is a no-op.
// Keep capture in the page and drain its bounded message queue over CDP.
export class CDP {
  constructor(socket) {
    this.socket = socket;
    this.pending = new Map();
    this.sequence = 0;
    socket.on('message', raw => {
      let message;
      try { message = JSON.parse(raw); } catch { return; }
      const pending = this.pending.get(message.id);
      if (!pending) return;
      this.pending.delete(message.id);
      clearTimeout(pending.timer);
      if (message.error) pending.reject(new Error('CDP command failed'));
      else pending.resolve(message.result);
    });
    socket.on('error', () => this.rejectPending());
    socket.on('close', () => this.rejectPending());
  }

  static async connect(endpoint) {
    const response = await fetch(new URL('/json/version', endpoint), { signal: AbortSignal.timeout(5000) });
    if (!response.ok) throw new Error('Foxbridge unavailable');
    const { webSocketDebuggerUrl } = await response.json();
    const debuggerURL = new URL(webSocketDebuggerUrl);
    debuggerURL.host = new URL(endpoint).host;
    const socket = new WebSocket(debuggerURL, { handshakeTimeout: 5000, maxPayload: 8 * 1024 * 1024 });
    const cdp = new CDP(socket);
    await new Promise((resolve, reject) => {
      socket.once('open', resolve);
      socket.once('error', reject);
    });
    return cdp;
  }

  rejectPending() {
    for (const { reject, timer } of this.pending.values()) {
      clearTimeout(timer);
      reject(new Error('Foxbridge disconnected'));
    }
    this.pending.clear();
  }

  send(method, params = {}, sessionId) {
    return new Promise((resolve, reject) => {
      if (this.socket.readyState !== WebSocket.OPEN) return reject(new Error('Foxbridge disconnected'));
      const id = ++this.sequence;
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error('CDP timeout'));
      }, 10000);
      this.pending.set(id, { resolve, reject, timer });
      this.socket.send(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) }));
    });
  }

  async evaluate(sessionId, expression) {
    const result = await this.send('Runtime.evaluate', { expression, returnByValue: true }, sessionId);
    if (result.exceptionDetails) throw new Error('Capture evaluation failed');
    return result.result?.value;
  }

  close() {
    this.rejectPending();
    this.socket.close();
  }
}
