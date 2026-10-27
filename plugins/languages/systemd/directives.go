package langsystemd

import "strings"

// Kind is the value shape a directive accepts (#3063). Highlighting reads only
// the directive names; completion (#3064) and lint (#3065) read the kinds and
// enum values.
type Kind int

const (
	KindString     Kind = iota // free text
	KindBool                   // yes/no/true/false/on/off/1/0
	KindEnum                   // one of Directive.Values
	KindInt                    // a plain integer
	KindTimespan               // a time span: "5s", "1min 30s", "infinity"
	KindSize                   // a byte size: "512M", "1G", "infinity"
	KindPath                   // a file-system path
	KindCommand                // a command line, optionally with prefixes (-@:+!)
	KindUnits                  // a space-separated list of unit names
	KindCalendar               // a calendar event expression ("daily", "Mon *-*-* 04:00")
	KindSignal                 // a signal name or number ("SIGTERM", "15")
	KindList                   // a space-separated list of words
	KindAssignment             // VAR=value pairs
	KindPercent                // a percentage or absolute value ("50%")
	KindMode                   // an octal file mode ("0644")
	KindAddress                // a listen address: port, host:port, path or @abstract
)

// Directive is one known `Name=` key of a section.
type Directive struct {
	Name   string
	Kind   Kind
	Values []string // enum values when Kind is KindEnum
	Doc    string   // one-line description
}

// Section is one `[Name]` section with the directives valid in it.
type Section struct {
	Name string
	// UnitTypes lists the unit-file extensions the section is valid in; nil
	// means every unit type.
	UnitTypes  []string
	Directives []Directive
}

// UnitTypes are the unit-file extensions the language recognises; each also
// names a drop-in directory `<name>.<type>.d`.
var UnitTypes = []string{
	"service", "socket", "timer", "path", "mount", "automount",
	"swap", "target", "slice", "scope", "device",
}

func d(name string, k Kind, doc string) Directive { return Directive{Name: name, Kind: k, Doc: doc} }

func enum(name, doc string, values ...string) Directive {
	return Directive{Name: name, Kind: KindEnum, Values: values, Doc: doc}
}

func join(groups ...[]Directive) []Directive {
	var out []Directive
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// execDirectives are the shared execution-environment directives
// (systemd.exec) of [Service], [Socket], [Mount] and [Swap].
var execDirectives = []Directive{
	d("WorkingDirectory", KindPath, "Working directory of executed processes (~ for the user's home)"),
	d("RootDirectory", KindPath, "Root directory for executed processes (chroot)"),
	d("RootImage", KindPath, "Disk image to mount as the root directory"),
	d("User", KindString, "User name or UID the processes run as"),
	d("Group", KindString, "Group name or GID the processes run as"),
	d("DynamicUser", KindBool, "Allocate a transient user and group for the unit"),
	d("SupplementaryGroups", KindList, "Additional groups the processes are members of"),
	d("PAMName", KindString, "PAM service name to open a session with"),
	d("UMask", KindMode, "File mode creation mask of executed processes"),
	d("Environment", KindAssignment, "Environment variables for executed processes (VAR=value …)"),
	d("EnvironmentFile", KindPath, "File to read environment variables from (- prefix: optional)"),
	d("PassEnvironment", KindList, "Variables passed through from the service manager's environment"),
	d("UnsetEnvironment", KindList, "Variables removed from the environment"),
	enum("StandardInput", "Where the process's standard input comes from",
		"null", "tty", "tty-force", "tty-fail", "data", "file:", "socket", "fd"),
	enum("StandardOutput", "Where the process's standard output goes",
		"inherit", "null", "tty", "journal", "kmsg", "journal+console", "kmsg+console", "file:", "append:", "truncate:", "socket", "fd"),
	enum("StandardError", "Where the process's standard error goes",
		"inherit", "null", "tty", "journal", "kmsg", "journal+console", "kmsg+console", "file:", "append:", "truncate:", "socket", "fd"),
	d("StandardInputText", KindString, "Text passed to standard input (StandardInput=data)"),
	d("SyslogIdentifier", KindString, "Process name prefixed to log lines"),
	enum("SyslogFacility", "Syslog facility for log messages",
		"kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp",
		"local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"),
	enum("SyslogLevel", "Default syslog level for log messages",
		"emerg", "alert", "crit", "err", "warning", "notice", "info", "debug"),
	enum("LogLevelMax", "Drop log messages above this level",
		"emerg", "alert", "crit", "err", "warning", "notice", "info", "debug"),
	d("TTYPath", KindPath, "Terminal device to use with StandardInput=tty"),
	d("Nice", KindInt, "Default nice level (-20…19)"),
	enum("CPUSchedulingPolicy", "CPU scheduling policy", "other", "batch", "idle", "fifo", "rr"),
	d("CPUSchedulingPriority", KindInt, "CPU scheduling priority (1…99 for fifo/rr)"),
	d("CPUAffinity", KindList, "CPUs the processes may run on"),
	enum("IOSchedulingClass", "I/O scheduling class", "realtime", "best-effort", "idle", "none"),
	d("IOSchedulingPriority", KindInt, "I/O scheduling priority (0…7)"),
	d("OOMScoreAdjust", KindInt, "OOM killer score adjustment (-1000…1000)"),
	d("LimitCPU", KindString, "RLIMIT_CPU: CPU time limit"),
	d("LimitFSIZE", KindSize, "RLIMIT_FSIZE: maximum file size"),
	d("LimitDATA", KindSize, "RLIMIT_DATA: maximum data segment size"),
	d("LimitSTACK", KindSize, "RLIMIT_STACK: maximum stack size"),
	d("LimitCORE", KindSize, "RLIMIT_CORE: maximum core dump size"),
	d("LimitNOFILE", KindString, "RLIMIT_NOFILE: maximum number of open files"),
	d("LimitNPROC", KindString, "RLIMIT_NPROC: maximum number of processes"),
	d("LimitMEMLOCK", KindSize, "RLIMIT_MEMLOCK: maximum locked memory"),
	d("LimitAS", KindSize, "RLIMIT_AS: maximum address space"),
	d("NoNewPrivileges", KindBool, "Forbid gaining privileges via execve()"),
	d("PrivateTmp", KindBool, "Private /tmp and /var/tmp for the processes"),
	d("PrivateDevices", KindBool, "Private minimal /dev without physical devices"),
	d("PrivateNetwork", KindBool, "Private network namespace with loopback only"),
	d("PrivateUsers", KindBool, "Private user namespace"),
	enum("ProtectSystem", "Mount /usr, /boot (and /etc) read-only", "yes", "no", "full", "strict"),
	enum("ProtectHome", "Make /home, /root and /run/user inaccessible or read-only", "yes", "no", "read-only", "tmpfs"),
	d("ProtectKernelTunables", KindBool, "Make /proc/sys, /sys and similar read-only"),
	d("ProtectKernelModules", KindBool, "Deny explicit kernel module loading"),
	d("ProtectKernelLogs", KindBool, "Deny access to the kernel log ring buffer"),
	d("ProtectControlGroups", KindBool, "Make the cgroup hierarchy read-only"),
	d("ProtectClock", KindBool, "Deny writes to the system clock"),
	d("ProtectHostname", KindBool, "Deny changing the hostname"),
	enum("ProtectProc", "Visibility of other processes in /proc", "noaccess", "invisible", "ptraceable", "default"),
	d("ReadWritePaths", KindList, "Paths writable despite ProtectSystem"),
	d("ReadOnlyPaths", KindList, "Paths mounted read-only"),
	d("InaccessiblePaths", KindList, "Paths made inaccessible"),
	d("BindPaths", KindList, "Bind mounts (source[:dest[:options]])"),
	d("BindReadOnlyPaths", KindList, "Read-only bind mounts"),
	d("TemporaryFileSystem", KindList, "Mount tmpfs at these paths"),
	d("RuntimeDirectory", KindList, "Directories created below /run"),
	d("StateDirectory", KindList, "Directories created below /var/lib"),
	d("CacheDirectory", KindList, "Directories created below /var/cache"),
	d("LogsDirectory", KindList, "Directories created below /var/log"),
	d("ConfigurationDirectory", KindList, "Directories created below /etc"),
	d("RuntimeDirectoryMode", KindMode, "Access mode of RuntimeDirectory"),
	d("StateDirectoryMode", KindMode, "Access mode of StateDirectory"),
	enum("RuntimeDirectoryPreserve", "Keep RuntimeDirectory after the unit stops", "yes", "no", "restart"),
	d("CapabilityBoundingSet", KindList, "Capability bounding set (~ prefix: deny list)"),
	d("AmbientCapabilities", KindList, "Ambient capabilities of executed processes"),
	d("SystemCallFilter", KindList, "Allowed (or ~ denied) system calls and @groups"),
	d("SystemCallArchitectures", KindList, "Allowed system call architectures (native, x86-64, …)"),
	d("SystemCallErrorNumber", KindString, "errno returned for filtered system calls"),
	d("RestrictAddressFamilies", KindList, "Allowed (or ~ denied) socket address families"),
	d("RestrictNamespaces", KindString, "Allowed namespace types (yes, no or a list)"),
	d("RestrictRealtime", KindBool, "Deny realtime scheduling"),
	d("RestrictSUIDSGID", KindBool, "Deny setting SUID/SGID bits"),
	d("LockPersonality", KindBool, "Lock the execution domain"),
	d("MemoryDenyWriteExecute", KindBool, "Deny writable and executable memory mappings"),
	d("RemoveIPC", KindBool, "Remove IPC objects of the user when the unit stops"),
	d("KeyringMode", KindString, "Kernel session keyring setup (inherit, private, shared)"),
	d("LoadCredential", KindString, "Credential loaded from a file (ID:PATH)"),
	d("SetCredential", KindString, "Credential set literally (ID:VALUE)"),
	d("SELinuxContext", KindString, "SELinux security context of executed processes"),
	d("AppArmorProfile", KindString, "AppArmor profile of executed processes"),
	enum("UtmpMode", "Kind of utmp/wtmp record written", "init", "login", "user"),
	d("UtmpIdentifier", KindString, "utmp/wtmp record identifier"),
	d("IgnoreSIGPIPE", KindBool, "Ignore SIGPIPE in executed processes"),
	d("TimerSlackNSec", KindTimespan, "Timer slack of executed processes"),
	enum("Personality", "Execution domain", "x86", "x86-64", "arm64", "arm64-be", "arm", "arm-be", "ppc", "ppc64", "ppc64-le", "s390", "s390x"),
}

// killDirectives are the shared process-killing directives (systemd.kill) of
// [Service], [Socket], [Mount], [Swap] and [Scope].
var killDirectives = []Directive{
	enum("KillMode", "Which processes are killed when the unit stops", "control-group", "mixed", "process", "none"),
	d("KillSignal", KindSignal, "Signal sent first to stop the unit"),
	d("RestartKillSignal", KindSignal, "Signal sent to stop the unit for a restart"),
	d("FinalKillSignal", KindSignal, "Signal sent to processes left after SIGKILL"),
	d("SendSIGKILL", KindBool, "Send SIGKILL to remaining processes after the timeout"),
	d("SendSIGHUP", KindBool, "Send SIGHUP right after KillSignal"),
	d("WatchdogSignal", KindSignal, "Signal sent when the watchdog times out"),
}

// resourceDirectives are the shared resource-control directives
// (systemd.resource-control) of [Service], [Socket], [Mount], [Swap],
// [Slice] and [Scope].
var resourceDirectives = []Directive{
	d("CPUAccounting", KindBool, "Account CPU usage"),
	d("CPUWeight", KindInt, "Relative CPU share (1…10000, default 100)"),
	d("StartupCPUWeight", KindInt, "CPU weight during startup and shutdown"),
	d("CPUQuota", KindPercent, "CPU time quota relative to one CPU (e.g. 50%)"),
	d("CPUQuotaPeriodSec", KindTimespan, "Period the CPU quota is measured over"),
	d("AllowedCPUs", KindList, "CPUs the unit may run on (cpuset)"),
	d("AllowedMemoryNodes", KindList, "NUMA nodes the unit may use"),
	d("MemoryAccounting", KindBool, "Account memory usage"),
	d("MemoryMin", KindSize, "Memory protected from reclaim"),
	d("MemoryLow", KindSize, "Best-effort memory protection"),
	d("MemoryHigh", KindSize, "Memory throttling limit"),
	d("MemoryMax", KindSize, "Absolute memory limit"),
	d("MemorySwapMax", KindSize, "Absolute swap limit"),
	d("TasksAccounting", KindBool, "Account the number of tasks"),
	d("TasksMax", KindPercent, "Maximum number of tasks (number, % or infinity)"),
	d("IOAccounting", KindBool, "Account block I/O"),
	d("IOWeight", KindInt, "Relative I/O weight (1…10000, default 100)"),
	d("StartupIOWeight", KindInt, "I/O weight during startup and shutdown"),
	d("IODeviceWeight", KindString, "Per-device I/O weight (device weight)"),
	d("IOReadBandwidthMax", KindString, "Per-device read bandwidth limit (device bytes)"),
	d("IOWriteBandwidthMax", KindString, "Per-device write bandwidth limit (device bytes)"),
	d("IOReadIOPSMax", KindString, "Per-device read IOPS limit (device iops)"),
	d("IOWriteIOPSMax", KindString, "Per-device write IOPS limit (device iops)"),
	d("IPAccounting", KindBool, "Account IP traffic"),
	d("IPAddressAllow", KindList, "Allowed IP address prefixes"),
	d("IPAddressDeny", KindList, "Denied IP address prefixes"),
	d("DeviceAllow", KindString, "Allowed device node and access (path rwm)"),
	enum("DevicePolicy", "Device access policy", "auto", "closed", "strict"),
	d("Slice", KindUnits, "Slice unit the unit is placed in"),
	d("Delegate", KindString, "Delegate cgroup control to the unit (yes, no or controllers)"),
	enum("ManagedOOMSwap", "systemd-oomd action on swap pressure", "auto", "kill"),
	enum("ManagedOOMMemoryPressure", "systemd-oomd action on memory pressure", "auto", "kill"),
	d("ManagedOOMMemoryPressureLimit", KindPercent, "Memory pressure limit for systemd-oomd"),
}

var unitSection = Section{Name: "Unit", Directives: []Directive{
	d("Description", KindString, "Human-readable name of the unit"),
	d("Documentation", KindList, "URIs of documentation (http:, https:, file:, info:, man:)"),
	d("Requires", KindUnits, "Hard dependencies: started with this unit, failure propagates"),
	d("Requisite", KindUnits, "Dependencies that must already be active"),
	d("Wants", KindUnits, "Soft dependencies started with this unit"),
	d("BindsTo", KindUnits, "Like Requires, and stop when they stop"),
	d("PartOf", KindUnits, "Stop and restart together with these units"),
	d("Upholds", KindUnits, "Keep these units running while this unit is active"),
	d("Conflicts", KindUnits, "Units that cannot run alongside this one"),
	d("Before", KindUnits, "Order this unit before these units"),
	d("After", KindUnits, "Order this unit after these units"),
	d("OnFailure", KindUnits, "Units activated when this unit fails"),
	d("OnSuccess", KindUnits, "Units activated when this unit succeeds"),
	d("PropagatesReloadTo", KindUnits, "Reload these units when this unit reloads"),
	d("ReloadPropagatedFrom", KindUnits, "Reload this unit when these units reload"),
	d("JoinsNamespaceOf", KindUnits, "Share namespaces with these units"),
	d("RequiresMountsFor", KindList, "Require and order after the mounts for these paths"),
	enum("OnFailureJobMode", "Job mode for OnFailure units",
		"fail", "replace", "replace-irreversibly", "isolate", "flush", "ignore-dependencies", "ignore-requirements"),
	d("IgnoreOnIsolate", KindBool, "Keep running when another unit is isolated"),
	d("StopWhenUnneeded", KindBool, "Stop when no other unit needs this unit"),
	d("RefuseManualStart", KindBool, "Refuse manual start requests"),
	d("RefuseManualStop", KindBool, "Refuse manual stop requests"),
	d("AllowIsolate", KindBool, "Allow `systemctl isolate` on this unit"),
	d("DefaultDependencies", KindBool, "Add the implicit default dependencies"),
	enum("CollectMode", "When the unit is garbage-collected", "inactive", "inactive-or-failed"),
	enum("FailureAction", "Action when the unit fails",
		"none", "reboot", "reboot-force", "reboot-immediate", "poweroff", "poweroff-force", "poweroff-immediate", "exit", "exit-force", "soft-reboot", "soft-reboot-force", "kexec", "kexec-force", "halt", "halt-force", "halt-immediate"),
	enum("SuccessAction", "Action when the unit succeeds",
		"none", "reboot", "reboot-force", "reboot-immediate", "poweroff", "poweroff-force", "poweroff-immediate", "exit", "exit-force", "soft-reboot", "soft-reboot-force", "kexec", "kexec-force", "halt", "halt-force", "halt-immediate"),
	d("JobTimeoutSec", KindTimespan, "Timeout for jobs of this unit"),
	d("JobRunningTimeoutSec", KindTimespan, "Timeout for running jobs of this unit"),
	d("StartLimitIntervalSec", KindTimespan, "Interval the start rate limit is measured over"),
	d("StartLimitBurst", KindInt, "Starts allowed within StartLimitIntervalSec"),
	enum("StartLimitAction", "Action when the start rate limit is hit",
		"none", "reboot", "reboot-force", "reboot-immediate", "poweroff", "poweroff-force", "poweroff-immediate", "exit", "exit-force"),
	d("SourcePath", KindPath, "Configuration file the unit was generated from"),
	d("ConditionPathExists", KindPath, "Start only if the path exists (! negates)"),
	d("ConditionPathExistsGlob", KindPath, "Start only if a path matches the glob"),
	d("ConditionPathIsDirectory", KindPath, "Start only if the path is a directory"),
	d("ConditionPathIsSymbolicLink", KindPath, "Start only if the path is a symlink"),
	d("ConditionPathIsMountPoint", KindPath, "Start only if the path is a mount point"),
	d("ConditionPathIsReadWrite", KindPath, "Start only if the path is on a writable file system"),
	d("ConditionDirectoryNotEmpty", KindPath, "Start only if the directory is not empty"),
	d("ConditionFileNotEmpty", KindPath, "Start only if the file exists and is not empty"),
	d("ConditionFileIsExecutable", KindPath, "Start only if the file is executable"),
	d("ConditionVirtualization", KindString, "Start only under this virtualization (yes, no, vm, container, kvm, …)"),
	d("ConditionArchitecture", KindString, "Start only on this architecture"),
	d("ConditionHost", KindString, "Start only on this hostname or machine ID"),
	d("ConditionKernelCommandLine", KindString, "Start only if the kernel command line has this option"),
	d("ConditionKernelVersion", KindString, "Start only for a matching kernel version"),
	d("ConditionSecurity", KindString, "Start only with this security technology (selinux, apparmor, …)"),
	d("ConditionCapability", KindString, "Start only with this capability in the bounding set"),
	d("ConditionACPower", KindBool, "Start only on (or off) AC power"),
	d("ConditionNeedsUpdate", KindPath, "Start only if /etc or /var needs an update"),
	d("ConditionFirstBoot", KindBool, "Start only on the first boot"),
	d("ConditionUser", KindString, "Start only for this user"),
	d("ConditionGroup", KindString, "Start only for this group"),
	d("ConditionEnvironment", KindAssignment, "Start only if the manager environment has this variable"),
	d("AssertPathExists", KindPath, "Fail unless the path exists"),
	d("AssertPathIsDirectory", KindPath, "Fail unless the path is a directory"),
	d("AssertFileNotEmpty", KindPath, "Fail unless the file exists and is not empty"),
	d("AssertVirtualization", KindString, "Fail unless running under this virtualization"),
	d("AssertUser", KindString, "Fail unless running for this user"),
}}

var installSection = Section{Name: "Install", Directives: []Directive{
	d("Alias", KindUnits, "Additional names the unit is installed under"),
	d("WantedBy", KindUnits, "Add a Wants= dependency from these units on enable"),
	d("RequiredBy", KindUnits, "Add a Requires= dependency from these units on enable"),
	d("UpheldBy", KindUnits, "Add an Upholds= dependency from these units on enable"),
	d("Also", KindUnits, "Units enabled and disabled together with this unit"),
	d("DefaultInstance", KindString, "Instance enabled for a template unit by default"),
}}

var serviceSection = Section{Name: "Service", UnitTypes: []string{"service"}, Directives: join([]Directive{
	enum("Type", "Process start-up type", "simple", "exec", "forking", "oneshot", "dbus", "notify", "notify-reload", "idle"),
	enum("ExitType", "When the service is considered finished", "main", "cgroup"),
	d("RemainAfterExit", KindBool, "Stay active after all processes exited"),
	d("GuessMainPID", KindBool, "Guess the main PID of a forking service"),
	d("PIDFile", KindPath, "PID file of a forking service"),
	d("BusName", KindString, "D-Bus name the service acquires"),
	d("ExecStart", KindCommand, "Command executed when the service starts"),
	d("ExecStartPre", KindCommand, "Command executed before ExecStart"),
	d("ExecStartPost", KindCommand, "Command executed after ExecStart"),
	d("ExecCondition", KindCommand, "Command deciding whether the service starts"),
	d("ExecReload", KindCommand, "Command executed to reload the service"),
	d("ExecStop", KindCommand, "Command executed to stop the service"),
	d("ExecStopPost", KindCommand, "Command executed after the service stopped"),
	d("RestartSec", KindTimespan, "Sleep before restarting the service"),
	d("RestartSteps", KindInt, "Steps to grow RestartSec to RestartMaxDelaySec"),
	d("RestartMaxDelaySec", KindTimespan, "Longest restart delay with RestartSteps"),
	d("TimeoutStartSec", KindTimespan, "Time allowed for start-up"),
	d("TimeoutStopSec", KindTimespan, "Time allowed for stopping"),
	d("TimeoutAbortSec", KindTimespan, "Time allowed for an aborting stop"),
	d("TimeoutSec", KindTimespan, "Sets TimeoutStartSec and TimeoutStopSec"),
	enum("TimeoutStartFailureMode", "Action when the start-up times out", "terminate", "abort", "kill"),
	enum("TimeoutStopFailureMode", "Action when stopping times out", "terminate", "abort", "kill"),
	d("RuntimeMaxSec", KindTimespan, "Maximum time the service may run"),
	d("RuntimeRandomizedExtraSec", KindTimespan, "Random extra time added to RuntimeMaxSec"),
	d("WatchdogSec", KindTimespan, "Watchdog timeout (the service pings via sd_notify)"),
	enum("Restart", "When the service is restarted",
		"no", "on-success", "on-failure", "on-abnormal", "on-watchdog", "on-abort", "always"),
	enum("RestartMode", "How dependencies are handled on restart", "normal", "direct"),
	d("SuccessExitStatus", KindList, "Exit codes and signals counted as success"),
	d("RestartPreventExitStatus", KindList, "Exit codes and signals that prevent a restart"),
	d("RestartForceExitStatus", KindList, "Exit codes and signals that force a restart"),
	d("RootDirectoryStartOnly", KindBool, "Apply RootDirectory to ExecStart only"),
	d("NonBlocking", KindBool, "Set O_NONBLOCK on passed sockets"),
	enum("NotifyAccess", "Which processes may send sd_notify messages", "none", "main", "exec", "all"),
	d("Sockets", KindUnits, "Socket units whose sockets the service inherits"),
	d("FileDescriptorStoreMax", KindInt, "File descriptors the service may store"),
	enum("FileDescriptorStorePreserve", "Keep stored file descriptors across restarts", "no", "yes", "restart"),
	d("USBFunctionDescriptors", KindPath, "USB FunctionFS descriptors file"),
	d("USBFunctionStrings", KindPath, "USB FunctionFS strings file"),
	enum("OOMPolicy", "Action when the kernel OOM killer hits a process", "continue", "stop", "kill"),
	d("OpenFile", KindString, "File opened and passed as a file descriptor"),
	d("ReloadSignal", KindSignal, "Signal sent for a reload (Type=notify-reload)"),
}, execDirectives, killDirectives, resourceDirectives)}

var socketSection = Section{Name: "Socket", UnitTypes: []string{"socket"}, Directives: join([]Directive{
	d("ListenStream", KindAddress, "Stream (TCP or AF_UNIX) listen address"),
	d("ListenDatagram", KindAddress, "Datagram (UDP or AF_UNIX) listen address"),
	d("ListenSequentialPacket", KindAddress, "Sequential-packet AF_UNIX listen address"),
	d("ListenFIFO", KindPath, "FIFO to listen on"),
	d("ListenSpecial", KindPath, "Special file (character device, /proc file) to listen on"),
	d("ListenNetlink", KindString, "Netlink family and group to listen on"),
	d("ListenMessageQueue", KindString, "POSIX message queue to listen on"),
	d("ListenUSBFunction", KindPath, "USB FunctionFS endpoint directory"),
	enum("SocketProtocol", "Socket protocol", "udplite", "sctp"),
	enum("BindIPv6Only", "IPv6 socket binding", "default", "both", "ipv6-only"),
	d("Backlog", KindInt, "Listen backlog length"),
	d("BindToDevice", KindString, "Network interface to bind to"),
	d("SocketUser", KindString, "Owner of AF_UNIX sockets and FIFOs"),
	d("SocketGroup", KindString, "Group of AF_UNIX sockets and FIFOs"),
	d("SocketMode", KindMode, "Access mode of AF_UNIX sockets and FIFOs"),
	d("DirectoryMode", KindMode, "Access mode of created parent directories"),
	d("Accept", KindBool, "Spawn one service instance per connection"),
	d("Writable", KindBool, "Open ListenSpecial files writable"),
	d("FlushPending", KindBool, "Flush pending data before activating the service"),
	d("MaxConnections", KindInt, "Concurrent connections allowed with Accept=yes"),
	d("MaxConnectionsPerSource", KindInt, "Concurrent connections per source IP"),
	d("KeepAlive", KindBool, "Enable TCP keep-alive"),
	d("KeepAliveTimeSec", KindTimespan, "TCP keep-alive idle time"),
	d("KeepAliveIntervalSec", KindTimespan, "TCP keep-alive probe interval"),
	d("KeepAliveProbes", KindInt, "TCP keep-alive probe count"),
	d("NoDelay", KindBool, "Disable Nagle's algorithm (TCP_NODELAY)"),
	d("Priority", KindInt, "Socket priority"),
	d("DeferAcceptSec", KindTimespan, "TCP_DEFER_ACCEPT timeout"),
	d("ReceiveBuffer", KindSize, "Socket receive buffer size"),
	d("SendBuffer", KindSize, "Socket send buffer size"),
	d("IPTOS", KindString, "IP type-of-service field"),
	d("IPTTL", KindInt, "IP time-to-live"),
	d("Mark", KindInt, "Firewall mark of packets"),
	d("ReusePort", KindBool, "Set SO_REUSEPORT"),
	d("SmackLabel", KindString, "SMACK label of the socket"),
	d("SELinuxContextFromNet", KindBool, "Derive the SELinux context from the network"),
	d("PipeSize", KindSize, "FIFO buffer size"),
	d("MessageQueueMaxMessages", KindInt, "Message queue mq_maxmsg"),
	d("MessageQueueMessageSize", KindInt, "Message queue mq_msgsize"),
	d("FreeBind", KindBool, "Allow binding to non-local addresses"),
	d("Transparent", KindBool, "Set IP_TRANSPARENT"),
	d("Broadcast", KindBool, "Allow broadcast datagrams"),
	d("PassCredentials", KindBool, "Set SO_PASSCRED"),
	d("PassSecurity", KindBool, "Set SO_PASSSEC"),
	d("PassPacketInfo", KindBool, "Set IP_PKTINFO"),
	enum("Timestamping", "Packet timestamping", "off", "us", "ns"),
	d("TCPCongestion", KindString, "TCP congestion algorithm"),
	d("ExecStartPre", KindCommand, "Command executed before the sockets are created"),
	d("ExecStartPost", KindCommand, "Command executed after the sockets are created"),
	d("ExecStopPre", KindCommand, "Command executed before the sockets are closed"),
	d("ExecStopPost", KindCommand, "Command executed after the sockets are closed"),
	d("TimeoutSec", KindTimespan, "Time allowed for the Exec commands"),
	d("Service", KindUnits, "Service unit activated by incoming traffic"),
	d("RemoveOnStop", KindBool, "Remove socket files and symlinks on stop"),
	d("Symlinks", KindList, "Symlinks created to AF_UNIX sockets and FIFOs"),
	d("FileDescriptorName", KindString, "Name of the passed file descriptors"),
	d("TriggerLimitIntervalSec", KindTimespan, "Interval the trigger rate limit is measured over"),
	d("TriggerLimitBurst", KindInt, "Activations allowed within TriggerLimitIntervalSec"),
}, execDirectives, killDirectives, resourceDirectives)}

var timerSection = Section{Name: "Timer", UnitTypes: []string{"timer"}, Directives: []Directive{
	d("OnActiveSec", KindTimespan, "Elapse relative to the timer's activation"),
	d("OnBootSec", KindTimespan, "Elapse relative to boot"),
	d("OnStartupSec", KindTimespan, "Elapse relative to the service manager's start-up"),
	d("OnUnitActiveSec", KindTimespan, "Elapse relative to the unit's last activation"),
	d("OnUnitInactiveSec", KindTimespan, "Elapse relative to the unit's last deactivation"),
	d("OnCalendar", KindCalendar, "Calendar event (daily, weekly, Mon *-*-* 04:00, …)"),
	d("AccuracySec", KindTimespan, "Timer accuracy (default 1min)"),
	d("RandomizedDelaySec", KindTimespan, "Random delay added to each elapse"),
	d("RandomizedOffsetSec", KindTimespan, "Stable random offset added to each elapse"),
	d("FixedRandomDelay", KindBool, "Keep the random delay stable across restarts"),
	d("OnClockChange", KindBool, "Elapse when the system clock jumps"),
	d("OnTimezoneChange", KindBool, "Elapse when the timezone changes"),
	d("Unit", KindUnits, "Unit activated when the timer elapses"),
	d("Persistent", KindBool, "Catch up on runs missed while powered off"),
	d("WakeSystem", KindBool, "Wake the system from suspend to elapse"),
	d("RemainAfterElapse", KindBool, "Stay loaded after elapsing"),
}}

var pathSection = Section{Name: "Path", UnitTypes: []string{"path"}, Directives: []Directive{
	d("PathExists", KindPath, "Activate when the path exists"),
	d("PathExistsGlob", KindPath, "Activate when a path matches the glob"),
	d("PathChanged", KindPath, "Activate when the file is closed after a write"),
	d("PathModified", KindPath, "Activate on every write to the file"),
	d("DirectoryNotEmpty", KindPath, "Activate when the directory holds a file"),
	d("Unit", KindUnits, "Unit activated when the path triggers"),
	d("MakeDirectory", KindBool, "Create watched directories"),
	d("DirectoryMode", KindMode, "Access mode of created directories"),
	d("TriggerLimitIntervalSec", KindTimespan, "Interval the trigger rate limit is measured over"),
	d("TriggerLimitBurst", KindInt, "Activations allowed within TriggerLimitIntervalSec"),
}}

var mountSection = Section{Name: "Mount", UnitTypes: []string{"mount"}, Directives: join([]Directive{
	d("What", KindPath, "Device, file or resource to mount"),
	d("Where", KindPath, "Mount point (must match the unit name)"),
	d("Type", KindString, "File system type"),
	d("Options", KindString, "Comma-separated mount options"),
	d("SloppyOptions", KindBool, "Tolerate unknown mount options"),
	d("LazyUnmount", KindBool, "Detach the file system lazily on unmount"),
	d("ReadWriteOnly", KindBool, "Fail instead of falling back to read-only"),
	d("ForceUnmount", KindBool, "Force the unmount"),
	d("DirectoryMode", KindMode, "Access mode of created mount-point directories"),
	d("TimeoutSec", KindTimespan, "Time allowed for the mount command"),
}, execDirectives, killDirectives, resourceDirectives)}

var automountSection = Section{Name: "Automount", UnitTypes: []string{"automount"}, Directives: []Directive{
	d("Where", KindPath, "Automount point (must match the unit name)"),
	d("ExtraOptions", KindString, "Extra options for the autofs mount"),
	d("DirectoryMode", KindMode, "Access mode of created mount-point directories"),
	d("TimeoutIdleSec", KindTimespan, "Unmount after this idle time"),
}}

var swapSection = Section{Name: "Swap", UnitTypes: []string{"swap"}, Directives: join([]Directive{
	d("What", KindPath, "Swap device or file"),
	d("Priority", KindInt, "Swap priority"),
	d("Options", KindString, "Comma-separated swapon options"),
	d("TimeoutSec", KindTimespan, "Time allowed for the swapon command"),
}, execDirectives, killDirectives, resourceDirectives)}

var sliceSection = Section{Name: "Slice", UnitTypes: []string{"slice"}, Directives: resourceDirectives}

var scopeSection = Section{Name: "Scope", UnitTypes: []string{"scope"}, Directives: join([]Directive{
	d("RuntimeMaxSec", KindTimespan, "Maximum time the scope may run"),
	d("RuntimeRandomizedExtraSec", KindTimespan, "Random extra time added to RuntimeMaxSec"),
	enum("OOMPolicy", "Action when the kernel OOM killer hits a process", "continue", "stop", "kill"),
}, killDirectives, resourceDirectives)}

// Sections is the directive table (#3063): every known section with its
// directives. Highlighting, completion (#3064) and lint (#3065) all read it.
var Sections = []Section{
	unitSection, installSection, serviceSection, socketSection, timerSection,
	pathSection, mountSection, automountSection, swapSection, sliceSection, scopeSection,
}

// index maps section name → directive name → directive.
var index = func() map[string]map[string]Directive {
	m := make(map[string]map[string]Directive, len(Sections))
	for _, s := range Sections {
		ds := make(map[string]Directive, len(s.Directives))
		for _, dir := range s.Directives {
			ds[dir.Name] = dir
		}
		m[s.Name] = ds
	}
	return m
}()

// LookupSection returns the section named name.
func LookupSection(name string) (Section, bool) {
	for _, s := range Sections {
		if s.Name == name {
			return s, true
		}
	}
	return Section{}, false
}

// Lookup returns the directive name of section. Names are case-sensitive, as
// in systemd.
func Lookup(section, name string) (Directive, bool) {
	dir, ok := index[section][name]
	return dir, ok
}

// IsExtension reports whether name is an `X-` extension name: systemd
// ignores X- sections and directives, so they are never unknown.
func IsExtension(name string) bool { return strings.HasPrefix(name, "X-") }
