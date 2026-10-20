const {readFileSync} = require('node:fs');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const source = readFileSync(process.argv[2], 'utf8');
const method = source.slice(source.indexOf('  _startSnapshotScreencast('), source.indexOf('  screencastFrameAck('));
let tick, frame, captureRect, renderArgs, capturedScale;
const canvas = {getContext:()=>({drawImage:(...args)=>{renderArgs=args}}), toDataURL:()=> 'data:image/jpeg;base64,frame'};
const Target = vm.runInNewContext(`(class { ${method} })`, {
  Services:{uuid:{generateUUID:()=>'{test}'}},
  PageTarget:{Events:{ScreencastFrame:'frame'}}, SNAPSHOT_SCREENCAST_FPS:5,
  setTimeout:fn=>{tick=fn;return 1},dump:message=>{throw Error(message)},
});
(async()=>{
  for (const scrollY of [0,200,9000]) {
    const target = new Target();
    target._window={document:{createElementNS:()=>canvas}};
    target._channel={connect:()=>({send:async()=>({width:1280,height:720,scrollX:0,scrollY})})};
    target.linkedBrowser=()=>({browsingContext:{currentWindowGlobal:{
      drawSnapshot:async(rect,scale)=>{captureRect=rect;capturedScale=scale;return {close(){}}},
    }}});
    target.emit=(name,event)=>{frame=event};
    target._startSnapshotScreencast({width:640,height:360,quality:70});
    await tick();
    assert.equal(captureRect,null,'capture must track the viewport, including fixed-position content, at every scroll offset');
    assert.equal(capturedScale,0.5);
    assert.equal(canvas.width,640);assert.equal(canvas.height,360);
    assert.equal(frame.viewportWidth,1280);assert.equal(frame.viewportHeight,720);
    assert.equal(frame.scrollOffsetY,scrollY);
    assert.deepEqual(renderArgs.slice(1),[0,0,640,360]);
  }
  console.log('Juggler screencast viewport regression tests passed');
})().catch(e=>{console.error(e);process.exitCode=1});
