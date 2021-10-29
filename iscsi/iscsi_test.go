package iscsi

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

var nodeDB = `
# BEGIN RECORD 6.2.0.874
node.name = iqn.2010-10.org.openstack:volume-eb393993-73d0-4e39-9ef4-b5841e244ced
node.tpgt = -1
node.startup = automatic
node.leading_login = No
iface.iscsi_ifacename = default
iface.transport_name = tcp
iface.vlan_id = 0
iface.vlan_priority = 0
iface.iface_num = 0
iface.mtu = 0
iface.port = 0
iface.tos = 0
iface.ttl = 0
iface.tcp_wsf = 0
iface.tcp_timer_scale = 0
iface.def_task_mgmt_timeout = 0
iface.erl = 0
iface.max_receive_data_len = 0
iface.first_burst_len = 0
iface.max_outstanding_r2t = 0
iface.max_burst_len = 0
node.discovery_port = 0
node.discovery_type = static
node.session.initial_cmdsn = 0
node.session.initial_login_retry_max = 8
node.session.xmit_thread_priority = -20
node.session.cmds_max = 128
node.session.queue_depth = 32
node.session.nr_sessions = 1
node.session.auth.authmethod = CHAP
node.session.auth.username = 86Jx6hXYqDYpKamtgx4d
node.session.auth.password = Qj3MuzmHu8cJBpkv
node.session.timeo.replacement_timeout = 120
node.session.err_timeo.abort_timeout = 15
node.session.err_timeo.lu_reset_timeout = 30
node.session.err_timeo.tgt_reset_timeout = 30
node.session.err_timeo.host_reset_timeout = 60
node.session.iscsi.FastAbort = Yes
node.session.iscsi.InitialR2T = No
node.session.iscsi.ImmediateData = Yes
node.session.iscsi.FirstBurstLength = 262144
node.session.iscsi.MaxBurstLength = 16776192
node.session.iscsi.DefaultTime2Retain = 0
node.session.iscsi.DefaultTime2Wait = 2
node.session.iscsi.MaxConnections = 1
node.session.iscsi.MaxOutstandingR2T = 1
node.session.iscsi.ERL = 0
node.conn[0].address = 192.168.1.107
node.conn[0].port = 3260
node.conn[0].startup = manual
node.conn[0].tcp.window_size = 524288
node.conn[0].tcp.type_of_service = 0
node.conn[0].timeo.logout_timeout = 15
node.conn[0].timeo.login_timeout = 15
node.conn[0].timeo.auth_timeout = 45
node.conn[0].timeo.noop_out_interval = 5
node.conn[0].timeo.noop_out_timeout = 5
node.conn[0].iscsi.MaxXmitDataSegmentLength = 0
node.conn[0].iscsi.MaxRecvDataSegmentLength = 262144
node.conn[0].iscsi.HeaderDigest = None
node.conn[0].iscsi.IFMarker = No
node.conn[0].iscsi.OFMarker = No
# END RECORD
`

var emptyTransportName = "iface.transport_name = \n"
var emptyDbRecord = "\n\n\n"
var testCmdOutput = ""
var testCmdTimeout = false
var testCmdError error
var testExecWithTimeoutError error
var mockedExitStatus = 0
var mockedStdout string

var sysBlockPath = "/sys/block"
var normalDevice = "sda"
var multipathDevice = "dm-1"
var slaves = []string{"sdb", "sdc"}

type testCmdRunner struct{}

func fakeExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestExecCommandHelper", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	es := strconv.Itoa(mockedExitStatus)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1",
		"STDOUT=" + mockedStdout,
		"EXIT_STATUS=" + es}
	return cmd
}

func fakeExecWithTimeout(command string, args []string, timeout time.Duration) ([]byte, error) {
	if testCmdTimeout {
		return nil, context.DeadlineExceeded
	}
	return []byte(testCmdOutput), testExecWithTimeoutError
}

func TestExecCommandHelper(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	fmt.Fprintf(os.Stdout, os.Getenv("STDOUT"))
	i, _ := strconv.Atoi(os.Getenv("EXIT_STATUS"))
	os.Exit(i)
}

func (tr testCmdRunner) execCmd(cmd string, args ...string) (string, error) {
	return testCmdOutput, testCmdError

}

func preparePaths(sysBlockPath string) error {
	for _, d := range append(slaves, normalDevice) {
		devicePath := filepath.Join(sysBlockPath, d, "device")
		err := os.MkdirAll(devicePath, os.ModePerm)
		if err != nil {
			return err
		}
		err = ioutil.WriteFile(filepath.Join(devicePath, "delete"), []byte(""), 0600)
		if err != nil {
			return err
		}
	}
	for _, s := range slaves {
		err := os.MkdirAll(filepath.Join(sysBlockPath, multipathDevice, "slaves", s), os.ModePerm)
		if err != nil {
			return err
		}
	}

	err := os.MkdirAll(filepath.Join(sysBlockPath, "dev", multipathDevice), os.ModePerm)
	if err != nil {
		return err
	}
	return nil

}

func Test_parseSessions(t *testing.T) {
	var sessions []iscsiSession
	output := "tcp: [2] 192.168.1.107:3260,1 iqn.2010-10.org.openstack:volume-eb393993-73d0-4e39-9ef4-b5841e244ced (non-flash)\n" +
		"tcp: [2] 192.168.1.200:3260,1 iqn.2010-10.org.openstack:volume-eb393993-73d0-4e39-9ef4-b5841e244ced (non-flash)\n"

	sessions = append(sessions,
		iscsiSession{
			Protocol: "tcp",
			ID:       2,
			Portal:   "192.168.1.107:3260",
			IQN:      "iqn.2010-10.org.openstack:volume-eb393993-73d0-4e39-9ef4-b5841e244ced",
			Name:     "volume-eb393993-73d0-4e39-9ef4-b5841e244ced",
		})
	sessions = append(sessions,
		iscsiSession{
			Protocol: "tcp",
			ID:       2,
			Portal:   "192.168.1.200:3260",
			IQN:      "iqn.2010-10.org.openstack:volume-eb393993-73d0-4e39-9ef4-b5841e244ced",
			Name:     "volume-eb393993-73d0-4e39-9ef4-b5841e244ced",
		})

	type args struct {
		lines string
	}
	validSession := args{
		lines: output,
	}
	tests := []struct {
		name string
		args args
		want []iscsiSession
	}{
		{"ValidParseSession", validSession, sessions},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSessions(tt.args.lines)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseSessions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_extractTransportName(t *testing.T) {
	type args struct {
		output string
	}
	validRecord := args{
		output: nodeDB,
	}
	emptyRecord := args{
		output: emptyDbRecord,
	}
	emptyTransportRecord := args{
		output: emptyTransportName,
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{"tcp-check", validRecord, "tcp"},
		{"tcp-check", emptyRecord, ""},
		{"tcp-check", emptyTransportRecord, "tcp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractTransportName(tt.args.output); got != tt.want {
				t.Errorf("extractTransportName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_sessionExists(t *testing.T) {
	mockedExitStatus = 0
	mockedStdout = "tcp: [4] 192.168.1.107:3260,1 iqn.2010-10.org.openstack:volume-eb393993-73d0-4e39-9ef4-b5841e244ced (non-flash)\n"
	execCommand = fakeExecCommand
	type args struct {
		tgtPortal string
		tgtIQN    string
	}
	testExistsArgs := args{
		tgtPortal: "192.168.1.107:3260",
		tgtIQN:    "iqn.2010-10.org.openstack:volume-eb393993-73d0-4e39-9ef4-b5841e244ced",
	}
	testWrongPortalArgs := args{
		tgtPortal: "10.0.0.1:3260",
		tgtIQN:    "iqn.2010-10.org.openstack:volume-eb393993-73d0-4e39-9ef4-b5841e244ced",
	}

	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{"TestSessionExists", testExistsArgs, true, false},
		{"TestSessionDoesNotExist", testWrongPortalArgs, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sessionExists(tt.args.tgtPortal, tt.args.tgtIQN)
			if (err != nil) != tt.wantErr {
				t.Errorf("sessionExists() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("sessionExists() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_DisconnectNormalVolume(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Errorf("can not create temp directory: %v", err)
		return
	}
	sysBlockPath = tmpDir
	defer os.RemoveAll(tmpDir)

	err = preparePaths(tmpDir)
	if err != nil {
		t.Errorf("can not create temp directories and files: %v", err)
		return
	}

	execWithTimeout = fakeExecWithTimeout
	devicePath := normalDevice

	tests := []struct {
		name         string
		removeDevice bool
		wantErr      bool
	}{
		{"DisconnectNormalVolume", false, false},
		{"DisconnectNonexistentNormalVolume", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.removeDevice {
				os.RemoveAll(filepath.Join(sysBlockPath, devicePath))
			}
			c := Connector{Multipath: false, DevicePath: devicePath}
			err := DisconnectVolume(c)
			if (err != nil) != tt.wantErr {
				t.Errorf("DisconnectVolume() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.removeDevice {
				deleteFile := filepath.Join(sysBlockPath, devicePath, "device", "delete")
				out, err := ioutil.ReadFile(deleteFile)
				if err != nil {
					t.Errorf("can not read file %v: %v", deleteFile, err)
					return
				}
				if string(out) != "1" {
					t.Errorf("file content mismatch, got = %s, want = 1", string(out))
					return
				}
			}
		})
	}
}

func Test_DisconnectMultipathVolume(t *testing.T) {

	execWithTimeout = fakeExecWithTimeout
	devicePath := multipathDevice

	tests := []struct {
		name         string
		timeout      bool
		removeDevice bool
		wantErr      bool
		cmdError     error
	}{
		{"DisconnectMultipathVolume", false, false, false, nil},
		{"DisconnectMultipathVolumeFlushFailed", false, false, true, fmt.Errorf("error")},
		{"DisconnectMultipathVolumeFlushTimeout", true, false, true, nil},
		{"DisconnectNonexistentMultipathVolume", false, true, false, fmt.Errorf("error")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			tmpDir, err := ioutil.TempDir("", "")
			if err != nil {
				t.Errorf("can not create temp directory: %v", err)
				return
			}
			sysBlockPath = tmpDir
			devPath = filepath.Join(tmpDir, "dev")
			defer os.RemoveAll(tmpDir)

			err = preparePaths(tmpDir)
			if err != nil {
				t.Errorf("can not create temp directories and files: %v", err)
				return
			}
			testExecWithTimeoutError = tt.cmdError
			testCmdTimeout = tt.timeout
			if tt.removeDevice {
				os.RemoveAll(filepath.Join(sysBlockPath, devicePath))
				os.RemoveAll(devPath)
			}
			c := Connector{Multipath: true, DevicePath: devicePath}
			err = DisconnectVolume(c)
			if (err != nil) != tt.wantErr {
				t.Errorf("DisconnectVolume() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.timeout {
				if err != context.DeadlineExceeded {
					t.Errorf("DisconnectVolume() error = %v, wantErr %v", err, context.DeadlineExceeded)
					return
				}
			}

			if !tt.removeDevice && !tt.wantErr {
				for _, s := range slaves {
					deleteFile := filepath.Join(sysBlockPath, s, "device", "delete")
					out, err := ioutil.ReadFile(deleteFile)
					if err != nil {
						t.Errorf("can not read file %v: %v", deleteFile, err)
						return
					}
					if string(out) != "1" {
						t.Errorf("file content mismatch, got = %s, want = 1", string(out))
						return
					}
				}

			}
		})
	}
}
