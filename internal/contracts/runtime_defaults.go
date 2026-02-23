package contracts

import "time"

const (
	DefaultIssuesRootDir  = ".issues"
	DefaultOpenDir        = ".issues/open"
	DefaultClosedDir      = ".issues/closed"
	DefaultSyncDir        = ".issues/.sync"
	DefaultOriginalsDir   = ".issues/.sync/originals"
	DefaultCacheFilePath  = ".issues/.sync/cache.json"
	DefaultConfigFilePath = ".issues/.sync/config.json"
	DefaultLockFilePath   = ".issues/.sync/lock"
)

const (
	DefaultPullPageSize     = 100
	DefaultPullConcurrency  = 4
	DefaultPushConcurrency  = 4
	DefaultHTTPTimeout      = 30 * time.Second
	DefaultRetryMaxAttempts = 3
	DefaultRetryBaseBackoff = 500 * time.Millisecond
)

const (
	DefaultLockStaleAfter     = 15 * time.Minute
	DefaultLockAcquireTimeout = 30 * time.Second
	DefaultLockPollInterval   = 200 * time.Millisecond
)

type CommandName string

const (
	CommandInit   CommandName = "init"
	CommandPull   CommandName = "pull"
	CommandPush   CommandName = "push"
	CommandSync   CommandName = "sync"
	CommandStatus CommandName = "status"
	CommandList   CommandName = "list"
	CommandNew    CommandName = "new"
	CommandEdit   CommandName = "edit"
	CommandView   CommandName = "view"
	CommandDiff   CommandName = "diff"
	CommandFields CommandName = "fields"
)

type LockRequirement string

const (
	LockRequirementNone      LockRequirement = "none"
	LockRequirementExclusive LockRequirement = "exclusive"
)

// CommandLockPolicy freezes lock requirements for each MVP command.
var CommandLockPolicy = map[CommandName]LockRequirement{
	CommandInit:   LockRequirementExclusive,
	CommandPull:   LockRequirementExclusive,
	CommandPush:   LockRequirementExclusive,
	CommandSync:   LockRequirementExclusive,
	CommandNew:    LockRequirementExclusive,
	CommandEdit:   LockRequirementExclusive,
	CommandStatus: LockRequirementNone,
	CommandList:   LockRequirementNone,
	CommandView:   LockRequirementNone,
	CommandDiff:   LockRequirementNone,
	CommandFields: LockRequirementNone,
}

func RequiresLock(command CommandName) bool {
	return CommandLockPolicy[command] == LockRequirementExclusive
}
