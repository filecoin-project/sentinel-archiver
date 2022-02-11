package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/filecoin-project/lily/commands"
	"github.com/filecoin-project/lily/lens/lily"
	"github.com/filecoin-project/lily/model/actors/common"
	init_ "github.com/filecoin-project/lily/model/actors/init"
	"github.com/filecoin-project/lily/model/actors/market"
	"github.com/filecoin-project/lily/model/actors/miner"
	"github.com/filecoin-project/lily/model/actors/multisig"
	"github.com/filecoin-project/lily/model/actors/power"
	"github.com/filecoin-project/lily/model/actors/reward"
	"github.com/filecoin-project/lily/model/actors/verifreg"
	"github.com/filecoin-project/lily/model/blocks"
	"github.com/filecoin-project/lily/model/chain"
	"github.com/filecoin-project/lily/model/derived"
	"github.com/filecoin-project/lily/model/messages"
	"github.com/filecoin-project/lily/model/msapprovals"
	"github.com/filecoin-project/lily/schedule"
	"github.com/ipfs/go-cid"
)

type Table struct {
	// Name is the name of the table
	Name string

	// Task is the name of the task that writes the table
	Task string

	// Schemas is the major schema version for which the table is supported.
	Schema int

	// An empty instance of the lily model
	Model interface{}
}

var TableList = []Table{
	{Name: "actor_states", Schema: 1, Task: "actorstatesraw", Model: &common.ActorState{}},
	{Name: "actors", Schema: 1, Task: "actorstatesraw", Model: &common.Actor{}},
	{Name: "block_headers", Schema: 1, Task: "blocks", Model: &blocks.BlockHeader{}},
	{Name: "block_messages", Schema: 1, Task: "messages", Model: &messages.BlockMessage{}},
	{Name: "block_parents", Schema: 1, Task: "blocks", Model: &blocks.BlockParent{}},
	{Name: "chain_consensus", Schema: 1, Task: "consensus", Model: &chain.ChainConsensus{}},
	{Name: "chain_economics", Schema: 1, Task: "chaineconomics", Model: &chain.ChainEconomics{}},
	{Name: "chain_powers", Schema: 1, Task: "actorstatespower", Model: &power.ChainPower{}},
	{Name: "chain_rewards", Schema: 1, Task: "actorstatesreward", Model: &reward.ChainReward{}},
	{Name: "derived_gas_outputs", Schema: 1, Task: "messages", Model: &derived.GasOutputs{}},
	{Name: "drand_block_entries", Schema: 1, Task: "blocks", Model: &blocks.DrandBlockEntrie{}},
	{Name: "id_addresses", Schema: 1, Task: "actorstatesinit", Model: &init_.IdAddress{}},
	{Name: "internal_messages", Schema: 1, Task: "implicitmessage", Model: &messages.InternalMessage{}},
	{Name: "internal_parsed_messages", Schema: 1, Task: "implicitmessage", Model: &messages.InternalParsedMessage{}},
	{Name: "market_deal_proposals", Schema: 1, Task: "actorstatesmarket", Model: &market.MarketDealProposal{}},
	{Name: "market_deal_states", Schema: 1, Task: "actorstatesmarket", Model: &market.MarketDealState{}},
	{Name: "message_gas_economy", Schema: 1, Task: "messages", Model: &messages.MessageGasEconomy{}},
	{Name: "messages", Schema: 1, Task: "messages", Model: &messages.Message{}},
	{Name: "miner_current_deadline_infos", Schema: 1, Task: "actorstatesminer", Model: &miner.MinerCurrentDeadlineInfo{}},
	{Name: "miner_fee_debts", Schema: 1, Task: "actorstatesminer", Model: &miner.MinerFeeDebt{}},
	{Name: "miner_infos", Schema: 1, Task: "actorstatesminer", Model: &miner.MinerInfo{}},
	{Name: "miner_locked_funds", Schema: 1, Task: "actorstatesminer", Model: &miner.MinerLockedFund{}},
	{Name: "miner_pre_commit_infos", Schema: 1, Task: "actorstatesminer", Model: &miner.MinerPreCommitInfo{}},
	{Name: "miner_sector_deals", Schema: 1, Task: "actorstatesminer", Model: &miner.MinerSectorDeal{}},
	{Name: "miner_sector_events", Schema: 1, Task: "actorstatesminer", Model: &miner.MinerSectorEvent{}},
	{Name: "miner_sector_infos_v7", Schema: 1, Task: "actorstatesminer", Model: &miner.MinerSectorInfo{}},  // added for actors v7 in network v15
	{Name: "miner_sector_infos", Schema: 1, Task: "actorstatesminer", Model: &miner.MinerSectorInfoV1_4{}}, // used for actors v6 and below, up to network v14
	{Name: "miner_sector_posts", Schema: 1, Task: "actorstatesminer", Model: &miner.MinerSectorPost{}},
	{Name: "multisig_approvals", Schema: 1, Task: "msapprovals", Model: &msapprovals.MultisigApproval{}},
	{Name: "multisig_transactions", Schema: 1, Task: "actorstatesmultisig", Model: &multisig.MultisigTransaction{}},
	{Name: "parsed_messages", Schema: 1, Task: "messages", Model: &messages.ParsedMessage{}},
	{Name: "power_actor_claims", Schema: 1, Task: "actorstatespower", Model: &power.PowerActorClaim{}},
	{Name: "receipts", Schema: 1, Task: "messages", Model: &messages.Receipt{}},
	{Name: "verified_registry_verifiers", Schema: 1, Task: "actorstatesverifreg", Model: &verifreg.VerifiedRegistryVerifier{}},
	{Name: "verified_registry_verified_clients", Schema: 1, Task: "actorstatesverifreg", Model: &verifreg.VerifiedRegistryVerifiedClient{}},
}

var (
	// TablesByName maps a table name to the table description.
	TablesByName = map[string]Table{}

	// KnownTasks is a lookup of known task names
	KnownTasks = map[string]struct{}{}

	// TablesBySchema maps a schema version to a list of tables present in that schema.
	TablesBySchema = map[int][]Table{}
)

func init() {
	for _, table := range TableList {
		TablesByName[table.Name] = table
		KnownTasks[table.Task] = struct{}{}
		TablesBySchema[table.Schema] = append(TablesBySchema[table.Schema], table)
	}
}

func TablesByTask(version int, task string) []Table {
	tables := []Table{}
	for _, table := range TableList {
		if table.Task == task {
			tables = append(tables, table)
		}
	}
	return tables
}

var ErrJobNotFound = errors.New("job not found")

type ExportManifest struct {
	Period  ExportPeriod
	Network string
	Files   []*ExportFile
}

func manifestForDate(ctx context.Context, d Date, network string, genesisTs int64, shipPath string, schemaVersion int, allowedTables []Table, compression Compression) (*ExportManifest, error) {
	p := firstExportPeriod(genesisTs)

	if p.Date.After(d) {
		return nil, fmt.Errorf("date is before genesis: %s", d.String())
	}

	// Iteration here guarantees we are always consistent with height ranges
	for p.Date != d {
		p = p.Next()
	}

	return manifestForPeriod(ctx, p, network, genesisTs, shipPath, schemaVersion, allowedTables, compression)
}

func manifestForPeriod(ctx context.Context, p ExportPeriod, network string, genesisTs int64, shipPath string, schemaVersion int, allowedTables []Table, compression Compression) (*ExportManifest, error) {
	em := &ExportManifest{
		Period:  p,
		Network: network,
	}

	for _, t := range TablesBySchema[schemaVersion] {
		allowed := false
		for i := range allowedTables {
			if allowedTables[i].Name == t.Name {
				allowed = true
				break
			}
		}
		if !allowed {
			continue
		}

		f := ExportFile{
			Date:        em.Period.Date,
			Schema:      schemaVersion,
			Network:     network,
			TableName:   t.Name,
			Format:      "csv", // hardcoded for now
			Compression: compression,
			Shipped:     true,
			Cid:         cid.Undef,
		}

		_, err := os.Stat(filepath.Join(shipPath, f.Path()))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				f.Shipped = false
			} else {
				return nil, fmt.Errorf("stat: %w", err)
			}
		}

		em.Files = append(em.Files, &f)
	}

	return em, nil
}

func (em *ExportManifest) FilterTables(allowed []Table) *ExportManifest {
	out := new(ExportManifest)
	out.Period = em.Period

	for _, f := range em.Files {
		include := false
		for _, t := range allowed {
			if f.TableName == t.Name {
				include = true
				break
			}
		}

		if include {
			out.Files = append(out.Files, f)
		}
	}

	return out
}

func (em *ExportManifest) HasUnshippedFiles() bool {
	for _, f := range em.Files {
		if !f.Shipped {
			return true
		}
	}
	return false
}

func (em *ExportManifest) FilesForTask(task string) []*ExportFile {
	var files []*ExportFile
	for _, ef := range em.Files {
		table, ok := TablesByName[ef.TableName]
		if !ok {
			// weird if we have an unknown table, but doesn't warrant exiting here
			continue
		}
		if table.Task == task {
			files = append(files, ef)
		}
	}

	return files
}

// ExportPeriod holds the parameters for an export covering a date.
type ExportPeriod struct {
	Date        Date
	StartHeight int64
	EndHeight   int64
}

// Next returns the following export period which cover the next calendar day.
func (e *ExportPeriod) Next() ExportPeriod {
	return ExportPeriod{
		Date:        e.Date.Next(),
		StartHeight: e.EndHeight + 1,
		EndHeight:   e.EndHeight + EpochsInDay,
	}
}

// firstExportPeriod returns the first period that should be exported. This is the period covering the day
// from genesis to 23:59:59 UTC the same day.
func firstExportPeriod(genesisTs int64) ExportPeriod {
	genesisDt := time.Unix(genesisTs, 0).UTC()
	midnightEpochAfterGenesis := midnightEpochForTs(genesisDt.AddDate(0, 0, 1).Unix(), genesisTs)

	return ExportPeriod{
		Date: Date{
			Year:  genesisDt.Year(),
			Month: int(genesisDt.Month()),
			Day:   genesisDt.Day(),
		},
		StartHeight: 0,
		EndHeight:   midnightEpochAfterGenesis - 1,
	}
}

// firstExportPeriodAfter returns the first period that should be exported at or after a specified minimum height.
func firstExportPeriodAfter(minHeight int64, genesisTs int64) ExportPeriod {
	// Iteration here guarantees we are always consistent with height ranges
	p := firstExportPeriod(genesisTs)
	for p.StartHeight < minHeight {
		p = p.Next()
	}

	return p
}

// Returns the height at midnight UTC (the start of the day) on the given date
func midnightEpochForTs(ts int64, genesisTs int64) int64 {
	t := time.Unix(ts, 0).UTC()
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return UnixToHeight(midnight.Unix(), genesisTs)
}

type ExportFile struct {
	Date        Date
	Schema      int
	Network     string
	TableName   string
	Format      string
	Compression Compression
	Shipped     bool // Shipped indicates that the file has been compressed and placed in the shared filesystem
	Cid         cid.Cid
}

// Path returns the path and file name that the export file should be written to.
func (e *ExportFile) Path() string {
	filename := e.Filename()
	return filepath.Join(e.Network, e.Format, strconv.Itoa(e.Schema), e.TableName, strconv.Itoa(e.Date.Year), filename)
}

// Filename returns file name that the export file should be written to.
func (e *ExportFile) Filename() string {
	return fmt.Sprintf("%s-%s.%s.%s", e.TableName, e.Date.String(), e.Format, e.Compression.Extension)
}

func (e *ExportFile) String() string {
	return fmt.Sprintf("%s-%s", e.TableName, e.Date.String())
}

// tasksForManifest calculates the visor tasks needed to produce the unshipped files in the supplied manifest
func tasksForManifest(em *ExportManifest) []string {
	tasks := make(map[string]struct{}, 0)

	for _, f := range em.Files {
		if f.Shipped {
			continue
		}
		t := TablesByName[f.TableName]
		tasks[t.Task] = struct{}{}
	}

	tasklist := make([]string, 0, len(tasks))
	for task := range tasks {
		tasklist = append(tasklist, task)
	}

	return tasklist
}

// walkForManifest creates a walk configuration for the given manifest
func walkForManifest(em *ExportManifest) (*lily.LilyWalkConfig, error) {
	walkName, err := unusedWalkName(storageConfig.path, em.Period.Date.String())
	if err != nil {
		return nil, fmt.Errorf("walk name: %w", err)
	}

	tasks := tasksForManifest(em)

	// Ensure we always produce the chain_consenus table
	hasConsensusTask := false
	for _, task := range tasks {
		if task == "consensus" {
			hasConsensusTask = true
			break
		}
	}
	if !hasConsensusTask {
		tasks = append(tasks, "consensus")
	}

	return &lily.LilyWalkConfig{
		Name:                walkName,
		Tasks:               tasks,
		Window:              0, // no time out
		From:                em.Period.StartHeight,
		To:                  em.Period.EndHeight,
		RestartDelay:        0,
		RestartOnCompletion: false,
		RestartOnFailure:    false,
		Storage:             storageConfig.name,
	}, nil
}

func unusedWalkName(exportPath, suffix string) (string, error) {
	walkName := fmt.Sprintf("arch%s-%s", time.Now().UTC().Format("0102"), suffix)
	for i := 0; i < 500; i++ {
		fname := exportFilePath(exportPath, walkName, "visor_processing_reports")
		_, err := os.Stat(fname)
		if errors.Is(err, os.ErrNotExist) {
			return walkName, nil
		}
		walkName = fmt.Sprintf("arch%s-%d-%s", time.Now().UTC().Format("0102"), rand.Intn(10000), suffix)
	}

	return "", fmt.Errorf("failed to find unusued walk name in a reasonable time")
}

// exportFilePath returns the full path to an export file
func exportFilePath(exportPath, prefix, name string) string {
	return filepath.Join(exportPath, fmt.Sprintf("%s-%s.csv", prefix, name))
}

func processExport(ctx context.Context, em *ExportManifest, shipPath string) error {
	ll := logger.With("date", em.Period.Date.String(), "from", em.Period.StartHeight, "to", em.Period.EndHeight)

	if !em.HasUnshippedFiles() {
		ll.Info("all files shipped, nothing to do")
		return nil
	}

	processExportStartedCounter.Inc()
	ll.Info("preparing to export files for shipping")

	// We must wait for one full finality after the end of the period before running the export
	earliestStartTs := HeightToUnix(em.Period.EndHeight+Finality, networkConfig.genesisTs)
	if time.Now().Unix() < earliestStartTs {
		ll.Infof("cannot start export until %s", time.Unix(earliestStartTs, 0).UTC().Format(time.RFC3339))
	}
	if err := WaitUntil(ctx, timeIsAfter(earliestStartTs), 0, time.Second*30); err != nil {
		return fmt.Errorf("failed waiting for earliest export time: %w", err)
	}

	var wi WalkInfo
	if err := WaitUntil(ctx, walkIsCompleted(lilyConfig.apiAddr, lilyConfig.apiToken, em, &wi, ll), 0, time.Second*30); err != nil {
		return fmt.Errorf("failed performing walk: %w", err)
	}

	ll.Info("export complete")
	report, err := verifyTasks(ctx, wi, tasksForManifest(em))
	if err != nil {
		return fmt.Errorf("failed to verify export files: %w", err)
	}

	shipFailure := false
	for task, ts := range report.TaskStatus {
		if !ts.IsOK() {
			verifyTableErrorsCounter.Inc()
			shipFailure = true
			continue
		}

		files := em.FilesForTask(task)
		for _, ef := range files {
			if !ef.Shipped {
				if err := shipExportFile(ctx, ef, wi, shipPath); err != nil {
					shipTableErrorsCounter.Inc()
					shipFailure = true
					ll.Errorw("failed to ship export file", "error", err)
					continue
				}

				if err := removeExportFile(ctx, ef, wi); err != nil {
					ll.Errorw("failed to remove export file", "error", err, "file", wi.WalkFile(ef.TableName))
				}
			}
		}
	}

	if shipFailure {
		return fmt.Errorf("failed to ship one or more export files")
	}

	return nil
}

func exportIsProcessed(p ExportPeriod, allowedTables []Table, compression Compression, shipPath string) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		em, err := manifestForPeriod(ctx, p, networkConfig.name, networkConfig.genesisTs, shipPath, storageConfig.schemaVersion, allowedTables, compression)
		if err != nil {
			processExportErrorsCounter.Inc()
			logger.Errorw("failed to create manifest", "error", err, "date", p.Date.String())
			return false, nil // force a retry
		}

		if err := processExport(ctx, em, shipPath); err != nil {
			processExportErrorsCounter.Inc()
			ll := logger.With("date", em.Period.Date.String(), "from", em.Period.StartHeight, "to", em.Period.EndHeight)
			ll.Errorw("failed to process export", "error", err)
			return false, nil // force a retry
		}

		return true, nil
	}
}

type basicLogger interface {
	Info(...interface{})
	Infof(string, ...interface{})
	Infow(string, ...interface{})
	Debug(...interface{})
	Debugf(string, ...interface{})
	Debugw(string, ...interface{})
	Error(...interface{})
	Errorf(string, ...interface{})
	Errorw(string, ...interface{})
}

func walkIsCompleted(apiAddr string, apiToken string, em *ExportManifest, walkInfo *WalkInfo, ll basicLogger) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		walkCfg, err := walkForManifest(em)
		if err != nil {
			walkErrorsCounter.Inc()
			ll.Errorf("failed to create walk configuration: %v")
			return false, nil
		}
		ll.Debugw(fmt.Sprintf("using tasks %s", strings.Join(walkCfg.Tasks, ",")), "walk", walkCfg.Name)

		var jobID schedule.JobID
		ll.Infow("starting walk", "walk", walkCfg.Name)
		if err := WaitUntil(ctx, jobHasBeenStarted(lilyConfig.apiAddr, lilyConfig.apiToken, walkCfg, &jobID, ll), 0, time.Second*30); err != nil {
			walkErrorsCounter.Inc()
			ll.Errorw(fmt.Sprintf("failed starting walk: %v", err), "walk", walkCfg.Name)
			return false, nil
		}

		ll.Infow("waiting for walk to complete", "walk", walkCfg.Name, "job_id", jobID)
		if err := WaitUntil(ctx, jobHasEnded(lilyConfig.apiAddr, lilyConfig.apiToken, jobID, ll), time.Second*30, time.Second*30); err != nil {
			walkErrorsCounter.Inc()
			ll.Errorw(fmt.Sprintf("failed waiting for walk to finish: %v", err), "walk", walkCfg.Name, "job_id", jobID)
			return false, nil
		}

		ll.Infow("walk complete", "walk", walkCfg.Name, "job_id", jobID)
		var jobListRes schedule.JobListResult
		if err := WaitUntil(ctx, jobGetResult(lilyConfig.apiAddr, lilyConfig.apiToken, walkCfg.Name, jobID, &jobListRes, ll), 0, time.Second*30); err != nil {
			walkErrorsCounter.Inc()
			ll.Errorw(fmt.Sprintf("failed waiting walk result: %v", err), "walk", walkCfg.Name, "job_id", jobID)
			return false, nil
		}

		if jobListRes.Error != "" {
			walkErrorsCounter.Inc()
			ll.Errorw(fmt.Sprintf("walk failed: %s", jobListRes.Error), "walk", walkCfg.Name, "job_id", jobID)
			return false, nil
		}

		wi := WalkInfo{
			Name:   walkCfg.Name,
			Path:   storageConfig.path,
			Format: "csv",
		}
		err = touchExportFiles(ctx, em, wi)
		if err != nil {
			walkErrorsCounter.Inc()
			ll.Errorw(fmt.Sprintf("failed to touch export files: %v", err), "walk", walkCfg.Name)
			return false, nil
		}

		*walkInfo = wi
		return true, nil
	}
}

func jobHasEnded(apiAddr string, apiToken string, id schedule.JobID, ll basicLogger) func(context.Context) (bool, error) {
	// To ensure this is robust in the case of a lily node restarting or being temporarily unresponsive, this
	// function opens its own api connection and never returns an error unless the job cannot be found
	return func(ctx context.Context) (bool, error) {
		api, closer, err := commands.GetAPI(ctx, apiAddr, apiToken)
		if err != nil {
			lilyConnectionErrorsCounter.Inc()
			ll.Errorf("failed to connect to lily api at %s: %v", apiAddr, err)
			return false, nil
		}
		defer closer()

		jr, err := getJobResult(ctx, api, id)
		if err != nil {
			lilyJobErrorsCounter.Inc()
			if errors.Is(err, ErrJobNotFound) {
				return false, err
			}
			ll.Errorf("failed to get job result", "error", err, "job_id", id)
			return false, nil
		}

		if jr.Running {
			return false, nil
		}
		return true, nil
	}
}

// note: jobID is an out parameter and the name in walkCfg may be updated with an existing name
func jobHasBeenStarted(apiAddr string, apiToken string, walkCfg *lily.LilyWalkConfig, jobID *schedule.JobID, ll basicLogger) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		api, closer, err := commands.GetAPI(ctx, apiAddr, apiToken)
		if err != nil {
			lilyConnectionErrorsCounter.Inc()
			ll.Errorf("failed to connect to lily api at %s: %v", apiAddr, err)
			return false, nil
		}
		defer closer()

		// Check if walk is already running
		jr, err := findExistingJob(ctx, api, walkCfg)
		if err != nil {
			if !errors.Is(err, ErrJobNotFound) {
				lilyJobErrorsCounter.Inc()
				ll.Errorw("failed to read jobs", "error", err)
				return false, nil
			}

			// No existing job, start a new walk
			res, err := api.LilyWalk(ctx, walkCfg)
			if err != nil {
				ll.Errorw("failed to create walk", "error", err, "walk", walkCfg.Name)
				return false, nil
			}

			// we're done
			*jobID = res.ID
			return true, err

		}

		ll.Infow("adopting running walk that matched required job", "job_id", jr.ID, "walk", jr.Name)
		*jobID = jr.ID
		walkCfg.Name = jr.Name
		return true, nil
	}
}

func jobGetResult(apiAddr string, apiToken string, walkName string, walkID schedule.JobID, jobListRes *schedule.JobListResult, ll basicLogger) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		api, closer, err := commands.GetAPI(ctx, apiAddr, apiToken)
		if err != nil {
			lilyConnectionErrorsCounter.Inc()
			ll.Errorf("failed to connect to lily api at %s: %v", apiAddr, err)
			return false, nil
		}
		defer closer()

		res, err := getJobResult(ctx, api, walkID)
		if err != nil {
			lilyJobErrorsCounter.Inc()
			ll.Errorf("failed reading job result for walk %s with id %d: %v", walkName, walkID, err)
			return false, nil
		}
		*jobListRes = *res
		return true, nil
	}
}

func timeIsAfter(targetTs int64) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		nowTs := time.Now().Unix()
		return nowTs > targetTs, nil
	}
}

func getJobResult(ctx context.Context, api lily.LilyAPI, id schedule.JobID) (*schedule.JobListResult, error) {
	jobs, err := api.LilyJobList(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	for _, jr := range jobs {
		if jr.ID == id {
			return &jr, nil
		}
	}

	return nil, ErrJobNotFound
}

func findExistingJob(ctx context.Context, api lily.LilyAPI, walkCfg *lily.LilyWalkConfig) (*schedule.JobListResult, error) {
	jobs, err := api.LilyJobList(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	for _, jr := range jobs {
		if jr.Type != "walk" {
			continue
		}

		if !jr.Running {
			continue
		}

		if jr.Params["storage"] != walkCfg.Storage {
			continue
		}

		from, err := strconv.ParseInt(jr.Params["minHeight"], 10, 64)
		if err != nil || from != walkCfg.From {
			continue
		}

		to, err := strconv.ParseInt(jr.Params["maxHeight"], 10, 64)
		if err != nil || to != walkCfg.To {
			continue
		}

		if !equalStringSlices(jr.Tasks, walkCfg.Tasks) {
			continue
		}

		return &jr, nil
	}

	return nil, ErrJobNotFound
}

type WalkInfo struct {
	Name   string // name of walk
	Path   string // storage output path
	Format string // usually csv
}

// WalkFile returns the path to the file that the walk would write for the given table
func (w *WalkInfo) WalkFile(table string) string {
	return filepath.Join(w.Path, fmt.Sprintf("%s-%s.%s", w.Name, table, w.Format))
}

// touchExportFiles ensures we have a zero length file for every export we expect from lily
func touchExportFiles(ctx context.Context, em *ExportManifest, wi WalkInfo) error {
	for _, ef := range em.Files {
		walkFile := wi.WalkFile(ef.TableName)
		f, err := os.OpenFile(walkFile, os.O_APPEND|os.O_CREATE, DefaultFilePerms)
		if err != nil {
			return fmt.Errorf("open file: %w", err)
		}
		f.Close()
	}

	return nil
}

func removeExportFiles(ctx context.Context, files []*ExportFile, wi WalkInfo) error {
	for _, ef := range files {
		walkFile := wi.WalkFile(ef.TableName)
		if err := os.Remove(walkFile); err != nil {
			logger.Errorf("failed to remove file %s: %w", walkFile, err)
		}
	}

	return nil
}

func removeExportFile(ctx context.Context, ef *ExportFile, wi WalkInfo) error {
	walkFile := wi.WalkFile(ef.TableName)
	if err := os.Remove(walkFile); err != nil {
		return fmt.Errorf("remove: %w", err)
	}

	return nil
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	acopy := make([]string, len(a))
	copy(acopy, a)
	sort.Strings(acopy)

	bcopy := make([]string, len(b))
	copy(bcopy, b)
	sort.Strings(bcopy)

	for i := range acopy {
		if acopy[i] != bcopy[i] {
			return false
		}
	}

	return true
}
