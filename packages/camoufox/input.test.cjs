// Execute the actual packaged Juggler handler with a renderer that drops no-op
// movement acknowledgements, as Gecko does. No browser or network is required.
const {readFileSync} = require('node:fs');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const source = readFileSync(process.argv[2], 'utf8');
const method = source.slice(source.indexOf("  async ['Page.dispatchMouseEvent']"), source.indexOf("  async ['Page.dispatchWheelEvent']"));
let sent, position, dropAck, watchers, observer;
class Watcher {
  constructor() { watchers.add(this); }
  ensureEvent(type) { return dropAck ? new Promise(() => {}) : Promise.resolve({type}); }
  dispose() { watchers.delete(this); }
  ensureEventsAndDispose() { this.dispose(); }
  hasEvent() { return false; }
}
const sandbox = {
  EventWatcher: Watcher, helper: {addObserver: fn => { observer = fn; return () => {observer = null}; }},
  // Accelerate the acknowledgement deadline only, keeping trajectory pacing.
  setTimeout: (fn, ms) => setTimeout(fn, ms === 2000 || ms === 1000 ? 10 : ms), clearTimeout,
  ChromeUtils: {camouGetBool: () => true, camouGetMouseTrajectory: () => [0, 0, 0, 0, 10, 10, 10, 10, 20, 20]},
};
const flushHelper = source.slice(source.indexOf('async function flushInputRepaints('), source.indexOf('function hashConsoleMessage('));
const Handler = vm.runInNewContext(`${flushHelper}; (class { ${method} })`, sandbox);
function fixture() {
  sent = []; position = {x:0,y:0}; dropAck = false; watchers = new Set();
  const h = new Handler();
  const win = {devicePixelRatio:1, windowUtils:{
    flushApzRepaints: () => false,
    jugglerSendMouseEvent: (type,x,y) => {
      sent.push([type,x,y]);
      dropAck = type === 'mousemove' && x === position.x && y === position.y;
      position = {x,y}; return sent.length;
    },
  }};
  h._lastTrackedPos = {x:0,y:0}; h._pendingEventWatchers = new Set();
  h._pageTarget = {_window:win, ensureContextMenuClosed(){}, activateAndRun: fn => fn(),
    _linkedBrowser:{scrollRectIntoViewIfNeeded(){},getBoundingClientRect:()=>({left:0,top:0,width:100,height:100})}};
  return h;
}
(async () => {
  const h = fixture();
  const send = (type,x,y) => h['Page.dispatchMouseEvent']({type,x,y,button:0,buttons:0,clickCount:1,modifiers:0});
  await send('mousemove',20,20);
  assert.deepEqual(sent.map(p=>p.slice(1)), [[10,10],[20,20]]);
  assert.equal(watchers.size,0);
  // Missing renderer acknowledgements reject and dispose their listener, then
  // the following input can run rather than staying behind a pending promise.
  h._pageTarget._window.windowUtils.jugglerSendMouseEvent = () => {dropAck=true;return 99};
  await assert.rejects(send('mousedown',30,30), /acknowledgement timed out/);
  assert.equal(watchers.size,0);
  h._pageTarget._window.windowUtils.jugglerSendMouseEvent = () => {dropAck=false;return 100};
  await send('mouseup',30,30);
  assert.equal(watchers.size,0);
  // APZ can notify synchronously during flush, or never notify at all.
  // Both must clean up their observer and let subsequent input proceed.
  const utils = h._pageTarget._window.windowUtils;
  utils.flushApzRepaints = () => {observer(); return true};
  await send('mousedown',40,40);
  assert.equal(observer,null);
  utils.flushApzRepaints = () => true;
  await send('mouseup',40,40);
  assert.equal(observer,null);
  console.log('Juggler input regression tests passed');
})().catch(e=>{console.error(e);process.exitCode=1});
