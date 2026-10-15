package terminal_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// Spec 0010 AC-12 and AC-13: writing hello is not proof that the child accepted it.
func verifyStartupDeadline(t *testing.T, executable string) {
	t.Helper()
	root := t.TempDir()
	copyPackage := func(source, destination string) {
		t.Helper()
		require.NoError(t, filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			target := filepath.Join(destination, relative)
			if entry.IsDir() {
				return os.MkdirAll(target, 0700)
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, info.Mode().Perm())
		}))
	}
	copyPackage(filepath.Join(filepath.Dir(executable), "lib"), filepath.Join(root, "lib"))
	binary, err := os.ReadFile(executable)
	require.NoError(t, err)
	exe := filepath.Join(root, "wpc")
	require.NoError(t, os.WriteFile(exe, binary, 0700))
	manifestPath := filepath.Join(root, "lib", "wpc-ui", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	var manifest map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &manifest))
	var files map[string]string
	require.NoError(t, json.Unmarshal(manifest["files"], &files))
	config := filepath.Join(root, "config.json")
	require.NoError(t, os.WriteFile(config, []byte(`{}`), 0600))
	const hello = `import {readSync,writeSync} from 'node:fs';
import {Socket} from 'node:net';
function read() {
  const byte=Buffer.alloc(1); let text='';
  while (readSync(4,byte,0,1,null)===1) { if(byte[0]===10)return JSON.parse(text); text+=byte.toString(); }
  process.exit(1);
}
function send(id,method,params) {writeSync(3,JSON.stringify({version:3,id,method,params})+'\n');}
send('1','hello',{release:process.argv[2],commit:process.argv[3],protocol:3});read();
`
	for _, tc := range []struct {
		name, script string
		code         int
	}{
		{"idle after hello", `const input=new Socket({fd:4,readable:true,writable:false});
input.on('end',()=>process.exit(1));input.resume();setInterval(()=>{},1000);`, 1},
		{"early quit", `setTimeout(()=>process.exit(0),100);`, 0},
		{"delayed overview", `setTimeout(()=>{send('2','overview',{});read();
setTimeout(()=>process.exit(0),2000);},4000);`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := []byte(hello + tc.script)
			require.NoError(t, os.WriteFile(filepath.Join(root, "lib/wpc-ui/app/dist/main.js"), entry, 0600))
			sum := sha256.Sum256(entry)
			files["app/dist/main.js"] = hex.EncodeToString(sum[:])
			manifest["files"], err = json.Marshal(files)
			require.NoError(t, err)
			data, err := json.Marshal(manifest)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(manifestPath, data, 0600))
			master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
			require.NoError(t, err)
			defer func() { _ = master.Close() }()
			require.NoError(t, unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0))
			number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
			require.NoError(t, err)
			slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
			require.NoError(t, err)
			defer func() { _ = slave.Close() }()
			before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
			require.NoError(t, err)
			cmd := exec.Command(exe, "--config", config, "--state-dir", filepath.Join(root, "absent"), "--color", "never")
			cmd.Env = append(os.Environ(), "TERM=xterm-256color", "NO_COLOR=1")
			cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
			require.NoError(t, cmd.Start())
			defer func() { _ = cmd.Process.Kill() }()
			var output terminalOutput
			go func() { _, _ = io.Copy(&output, master) }()
			exited := make(chan error, 1)
			go func() { exited <- cmd.Wait() }()
			select {
			case <-exited:
			case <-time.After(8 * time.Second):
				_ = cmd.Process.Kill()
				<-exited
				t.Fatal("parent did not bound startup after sending hello")
			}
			require.Equal(t, tc.code, cmd.ProcessState.ExitCode(), output.text())
			if tc.code == 1 {
				require.Contains(t, output.text(), "ui_startup_timeout")
			}
			after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
