import {render, type Instance} from 'ink';
import {Component, type ReactNode} from 'react';
import {Channel} from './channel.js';
import {hello, invalid} from './schema.js';
import {Screen} from './screen.js';

const channel = new Channel();
let instance: Instance | undefined;
let closing = false;
let parent = 0;
function quit(code: number, interrupt = false) {
  if (closing) return;
  closing = true;
  if (interrupt && channel.connected && parent === process.ppid && parent > 1) {
    try { process.kill(parent, 'SIGINT'); } catch { /* Parent may have just exited. */ }
  }
  // Parent owns a second restoration path if this process cannot clean up.
  const timer = setTimeout(() => process.exit(code), 1500);
  try { instance?.unmount(); } catch { /* Only approved exit facts leave this process. */ }
  channel.close();
  void (instance?.waitUntilExit() ?? Promise.resolve()).catch(() => {}).finally(() => {
    clearTimeout(timer);
    process.exit(code);
  });
}
channel.onFailure = () => quit(1);
process.on('SIGINT', () => quit(130));
process.on('SIGTERM', () => quit(143));
process.on('uncaughtException', () => quit(1));
process.on('unhandledRejection', () => quit(1));
class SafeBoundary extends Component<{children: ReactNode}, {failed: boolean}> {
  state = {failed: false};
  static getDerivedStateFromError() { return {failed: true}; }
  componentDidCatch() { quit(1); }
  render() { return this.state.failed ? null : this.props.children; }
}
try {
  const release = process.argv[2] ?? '';
  const commit = process.argv[3] ?? '';
  const welcome = hello(await channel.request('hello', {release, commit, protocol: 1}));
  if (welcome.release !== release || welcome.commit !== commit || welcome.parent_pid !== String(process.ppid) ||
    welcome.refresh_delay_ns !== '5000000000' || welcome.profile.suggestions[0]?.action !== 'sources' ||
    welcome.profile.suggestions[1]?.action !== 'crawl' || !process.stdin.isTTY || !process.stdout.isTTY) invalid();
  parent = process.ppid;
  instance = render(<SafeBoundary><Screen channel={channel} hello={welcome} quit={quit}/></SafeBoundary>,
    {exitOnCtrlC: false, patchConsole: false, alternateScreen: true, interactive: true});
  await instance.waitUntilExit();
} catch { quit(1); }
