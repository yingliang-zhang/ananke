pub mod generated;

use generated::{
    renderer_public_bootstrap::{Bootstrap, Project, Workstream},
    renderer_public_cancel::Cancel,
    renderer_public_event::Event,
    renderer_public_health::Health,
    renderer_public_proposal_activity_list::ProposalActivityList,
    renderer_public_proposal_activity_list_input::ListProposalActivityInput,
    renderer_public_proposal_append_input::{
        AppendProposalRevisionInput, AppendProposalRevisionInputBody,
    },
    renderer_public_proposal_create_input::{CreateProposalInput, CreateProposalRevisionInput},
    renderer_public_proposal_decision_input::{DecideProposalApprovalInput, Decision},
    renderer_public_proposal_detail::ProposalDetail,
    renderer_public_proposal_get_input::GetProposalInput,
    renderer_public_proposal_list::ProposalList,
    renderer_public_proposal_list_input::ListProposalsInput,
    renderer_public_proposal_mutation::ProposalMutation,
    renderer_public_proposal_withdraw_input::WithdrawProposalInput,
    renderer_public_run::{Run, RunDiagnostics},
};
use serde::{Deserialize, Serialize, de::DeserializeOwned};
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Write};
use std::os::unix::fs::symlink;
use std::os::unix::fs::{FileTypeExt, MetadataExt, OpenOptionsExt, PermissionsExt};
use std::os::unix::net::UnixStream;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::Mutex;
use std::thread;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};
use tauri::{AppHandle, Manager, State};

const DEFAULT_PROJECT_ID: &str = "ananke";
const DEFAULT_PROJECT_NAME: &str = "Ananke";
const DEFAULT_WORKSTREAM_ID: &str = "main";
const DEFAULT_WORKSTREAM_NAME: &str = "main";
const PRIVATE_RUNTIME_PREFIX: &str = "ananke-gui-";
const DAEMON_SOCKET_NAME: &str = "daemon.sock";
const DATA_SOCKET_ALIAS_NAME: &str = "data";
const TOKEN_FILE_NAME: &str = "daemon-token";
const DAEMON_START_TIMEOUT: Duration = Duration::from_secs(5);
const API_TIMEOUT: Duration = Duration::from_secs(5);

#[derive(Debug)]
enum BridgeError {
    Io(std::io::Error),
    SocketConnect(std::io::Error),
    Json(serde_json::Error),
    InvalidToken,
    UnsafeRuntimeDirectory,
    UnexpectedSocketEndpoint,
    DataAliasMismatch,
    MissingBinary,
    DaemonRejected(String),
    Protocol,
    DaemonUnavailable,
}

impl From<std::io::Error> for BridgeError {
    fn from(error: std::io::Error) -> Self {
        Self::Io(error)
    }
}

impl From<serde_json::Error> for BridgeError {
    fn from(error: serde_json::Error) -> Self {
        Self::Json(error)
    }
}

impl std::fmt::Display for BridgeError {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter.write_str("Ananke daemon bridge error")
    }
}

impl std::error::Error for BridgeError {}

impl BridgeError {
    fn public_message(&self) -> String {
        match self {
            Self::DaemonRejected(_) => "The daemon rejected this request.".into(),
            Self::Io(error) | Self::SocketConnect(error) => {
                let _ = error.kind();
                "The Ananke daemon is unavailable. Check the local backend installation and retry."
                    .into()
            }
            Self::Json(error) => {
                let _ = error.classify();
                "The Ananke daemon is unavailable. Check the local backend installation and retry."
                    .into()
            }
            Self::InvalidToken
            | Self::UnsafeRuntimeDirectory
            | Self::UnexpectedSocketEndpoint
            | Self::DataAliasMismatch
            | Self::MissingBinary
            | Self::DaemonUnavailable
            | Self::Protocol => {
                "The Ananke daemon is unavailable. Check the local backend installation and retry."
                    .into()
            }
        }
    }
}

fn is_credible_stale_socket_failure(error: &BridgeError) -> bool {
    matches!(
        error,
        BridgeError::SocketConnect(error)
            if matches!(
                error.kind(),
                std::io::ErrorKind::NotFound | std::io::ErrorKind::ConnectionRefused
            )
    )
}

#[derive(Clone)]
struct DaemonPaths {
    app_data_dir: PathBuf,
    runtime_dir: PathBuf,
    data_socket_alias: PathBuf,
    socket: PathBuf,
    daemon_binary: PathBuf,
    supervisor_binary: PathBuf,
    fakeworker_binary: PathBuf,
    repository_root: PathBuf,
}

impl DaemonPaths {
    fn from_app(app: &AppHandle) -> Result<Self, BridgeError> {
        let app_data_dir = app
            .path()
            .app_data_dir()
            .map_err(|_| BridgeError::DaemonUnavailable)?;
        let repository_root = current_project_root(&app_data_dir)?;
        #[cfg(debug_assertions)]
        let binaries_dir = development_repository_root().join(".ananke/bin");
        #[cfg(not(debug_assertions))]
        let binaries_dir = app
            .path()
            .resource_dir()
            .map_err(|_| BridgeError::DaemonUnavailable)?
            .join("ananke-bin");
        Self::from_parts(
            app_data_dir,
            repository_root,
            binaries_dir,
            private_runtime_directory(),
        )
    }

    fn from_parts(
        app_data_dir: PathBuf,
        repository_root: PathBuf,
        binaries_dir: PathBuf,
        runtime_dir: PathBuf,
    ) -> Result<Self, BridgeError> {
        ensure_owned_private_directory(&app_data_dir)?;
        let data_dir = app_data_dir.join("runs");
        ensure_owned_private_directory(&data_dir)?;
        ensure_private_runtime_dir(&runtime_dir)?;
        let data_socket_alias = runtime_dir.join(DATA_SOCKET_ALIAS_NAME);
        ensure_data_alias(&data_dir, &data_socket_alias)?;
        let socket = runtime_dir.join(DAEMON_SOCKET_NAME);
        Ok(Self {
            app_data_dir,
            runtime_dir,
            data_socket_alias,
            socket,
            daemon_binary: binaries_dir.join("ananke"),
            supervisor_binary: binaries_dir.join("ananke-supervisor"),
            fakeworker_binary: binaries_dir.join("ananke-fakeworker"),
            repository_root,
        })
    }

    fn store_path(&self) -> PathBuf {
        self.app_data_dir.join("journal.sqlite")
    }

    fn token_path(&self) -> PathBuf {
        self.app_data_dir.join(TOKEN_FILE_NAME)
    }
}

fn effective_uid() -> u32 {
    // SAFETY: geteuid has no preconditions and does not access Rust-managed memory.
    unsafe { libc::geteuid() }
}

fn private_runtime_directory() -> PathBuf {
    PathBuf::from("/tmp").join(format!("{PRIVATE_RUNTIME_PREFIX}{}", effective_uid()))
}

fn ensure_private_runtime_dir(path: &Path) -> Result<(), BridgeError> {
    ensure_owned_private_directory(path)
}

fn ensure_owned_private_directory(path: &Path) -> Result<(), BridgeError> {
    match fs::symlink_metadata(path) {
        Ok(_) => {}
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => fs::create_dir_all(path)?,
        Err(error) => return Err(error.into()),
    }
    let metadata = fs::symlink_metadata(path)?;
    validate_owned_private_directory(&metadata)?;
    fs::set_permissions(path, fs::Permissions::from_mode(0o700))?;
    let metadata = fs::symlink_metadata(path)?;
    validate_owned_private_directory(&metadata)?;
    if metadata.permissions().mode() & 0o777 != 0o700 {
        return Err(BridgeError::UnsafeRuntimeDirectory);
    }
    Ok(())
}

fn validate_owned_private_directory(metadata: &fs::Metadata) -> Result<(), BridgeError> {
    if metadata.file_type().is_symlink()
        || !metadata.file_type().is_dir()
        || metadata.uid() != effective_uid()
    {
        return Err(BridgeError::UnsafeRuntimeDirectory);
    }
    Ok(())
}

fn select_project_root(
    app_data_dir: &Path,
    development_root: &Path,
    debug_build: bool,
) -> Result<PathBuf, BridgeError> {
    ensure_owned_private_directory(app_data_dir)?;
    if debug_build {
        return Ok(development_root.to_path_buf());
    }
    let project_root = app_data_dir.join("project-root");
    ensure_owned_private_directory(&project_root)?;
    Ok(project_root)
}

#[cfg(debug_assertions)]
fn development_repository_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .and_then(Path::parent)
        .expect("GUI crate is nested under the repository root")
        .to_path_buf()
}

fn current_project_root(app_data_dir: &Path) -> Result<PathBuf, BridgeError> {
    #[cfg(debug_assertions)]
    {
        return select_project_root(app_data_dir, &development_repository_root(), true);
    }
    #[cfg(not(debug_assertions))]
    {
        select_project_root(app_data_dir, Path::new(""), false)
    }
}

// The actual run data lives in the app-data directory. The short, verified
// symlink is passed to Go so per-run supervisor socket paths remain below the
// Darwin Unix-domain socket limit.
fn ensure_data_alias(data_dir: &Path, alias: &Path) -> Result<(), BridgeError> {
    match fs::symlink_metadata(alias) {
        Ok(metadata) if metadata.file_type().is_symlink() => {
            let destination = fs::canonicalize(alias)?;
            if destination != fs::canonicalize(data_dir)? {
                return Err(BridgeError::DataAliasMismatch);
            }
        }
        Ok(_) => return Err(BridgeError::DataAliasMismatch),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => symlink(data_dir, alias)?,
        Err(error) => return Err(error.into()),
    }
    Ok(())
}

fn load_or_create_token(path: &Path) -> Result<String, BridgeError> {
    match fs::read_to_string(path) {
        Ok(token) => {
            let token = token.trim().to_owned();
            if token.len() != 64 || !token.bytes().all(|byte| byte.is_ascii_hexdigit()) {
                return Err(BridgeError::InvalidToken);
            }
            let mode = fs::metadata(path)?.permissions().mode() & 0o777;
            if mode != 0o600 {
                fs::set_permissions(path, fs::Permissions::from_mode(0o600))?;
            }
            Ok(token)
        }
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => create_token(path),
        Err(error) => Err(error.into()),
    }
}

fn create_token(path: &Path) -> Result<String, BridgeError> {
    let mut entropy = [0_u8; 32];
    File::open("/dev/urandom")?.read_exact(&mut entropy)?;
    let mut token = String::with_capacity(entropy.len() * 2);
    for byte in entropy {
        use std::fmt::Write as _;
        write!(&mut token, "{byte:02x}").expect("writing to a String cannot fail");
    }
    let mut token_file = match OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(path)
    {
        Ok(file) => file,
        Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => {
            return load_or_create_token(path);
        }
        Err(error) => return Err(error.into()),
    };
    token_file.write_all(token.as_bytes())?;
    token_file.write_all(b"\n")?;
    token_file.sync_all()?;
    Ok(token)
}

#[derive(Serialize)]
struct GoRequest<'a> {
    cmd: &'a str,
    token: &'a str,
    #[serde(skip_serializing_if = "Option::is_none")]
    id: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    name: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    root: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    project_id: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    workstream_id: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    worker_path: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    worker_args: Option<&'a [String]>,
    #[serde(skip_serializing_if = "Option::is_none")]
    worker_env: Option<&'a [String]>,
    #[serde(skip_serializing_if = "Option::is_none")]
    after_seq: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    proposal: Option<GoProposalRequest<'a>>,
}

impl<'a> GoRequest<'a> {
    fn new(cmd: &'a str, token: &'a str) -> Self {
        Self {
            cmd,
            token,
            id: None,
            name: None,
            root: None,
            project_id: None,
            workstream_id: None,
            worker_path: None,
            worker_args: None,
            worker_env: None,
            after_seq: None,
            proposal: None,
        }
    }
}

#[derive(Debug, Deserialize)]
struct GoResponse {
    ok: bool,
    #[serde(default)]
    error: Option<String>,
    #[serde(default)]
    state: Option<String>,
    #[serde(default)]
    run: Option<JsonRun>,
    #[serde(default)]
    runs: Vec<JsonRun>,
    #[serde(default)]
    events: Vec<EventDto>,
    #[serde(default)]
    accepted: bool,
    #[serde(default)]
    proposal_mutation: Option<serde_json::Value>,
    #[serde(default)]
    proposals: Option<serde_json::Value>,
    #[serde(default)]
    proposal_detail: Option<serde_json::Value>,
    #[serde(default)]
    proposal_activity: Option<serde_json::Value>,
}
// GoProposalRequest and its nested records are private bridge transport.
// Generated renderer-public types are converted at the Tauri edge below.
#[derive(Serialize)]
struct GoProposalRequest<'a> {
    #[serde(skip_serializing_if = "Option::is_none")]
    idempotency_key: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    project_id: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    workstream_id: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    proposal_id: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    expected_current_revision: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    expected_current_revision_hash: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    revision_input: Option<GoProposalRevisionInput<'a>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    approval_id: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    revision: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    revision_hash: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    decision: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    reason: Option<&'a str>,
}

#[derive(Serialize)]
struct GoProposalRevisionInput<'a> {
    task: GoProposalTask<'a>,
    acceptance_criteria: &'a [String],
    policy: GoProposalPolicy<'a>,
}

#[derive(Serialize)]
struct GoProposalTask<'a> {
    title: &'a str,
    instructions: &'a str,
}

#[derive(Serialize)]
struct GoProposalPolicy<'a> {
    adapter: GoProposalAdapterPolicy,
    authority: &'static str,
    budget: GoProposalBudgetPolicy<'a>,
    model_role: &'static str,
}

#[derive(Serialize)]
struct GoProposalAdapterPolicy {
    access: &'static str,
    kind: &'static str,
    status: &'static str,
}

#[derive(Serialize)]
struct GoProposalBudgetPolicy<'a> {
    dimensions: &'a [String],
    status: &'static str,
}

impl<'a> GoProposalRequest<'a> {
    fn empty() -> Self {
        Self {
            idempotency_key: None,
            project_id: None,
            workstream_id: None,
            proposal_id: None,
            expected_current_revision: None,
            expected_current_revision_hash: None,
            revision_input: None,
            approval_id: None,
            revision: None,
            revision_hash: None,
            decision: None,
            reason: None,
        }
    }
}

fn create_proposal_request(input: &CreateProposalInput) -> GoProposalRequest<'_> {
    let mut request = GoProposalRequest::empty();
    request.idempotency_key = Some(&input.idempotency_key);
    request.project_id = Some(&input.project_id);
    request.workstream_id = Some(&input.workstream_id);
    request.revision_input = Some(create_revision_input(&input.revision_input));
    request
}

fn append_proposal_request(input: &AppendProposalRevisionInput) -> GoProposalRequest<'_> {
    let mut request = GoProposalRequest::empty();
    request.idempotency_key = Some(&input.idempotency_key);
    request.proposal_id = Some(&input.proposal_id);
    request.expected_current_revision = Some(input.expected_current_revision);
    request.expected_current_revision_hash = Some(&input.expected_current_revision_hash);
    request.revision_input = Some(append_revision_input(&input.revision_input));
    request
}

fn decision_proposal_request(input: &DecideProposalApprovalInput) -> GoProposalRequest<'_> {
    let mut request = GoProposalRequest::empty();
    request.idempotency_key = Some(&input.idempotency_key);
    request.approval_id = Some(&input.approval_id);
    request.proposal_id = Some(&input.proposal_id);
    request.revision = Some(input.revision);
    request.revision_hash = Some(&input.revision_hash);
    request.decision = Some(match &input.decision {
        Decision::Approved => "approved",
        Decision::Rejected => "rejected",
    });
    request.reason = Some(&input.reason);
    request
}
fn create_revision_input(input: &CreateProposalRevisionInput) -> GoProposalRevisionInput<'_> {
    GoProposalRevisionInput {
        task: GoProposalTask {
            title: &input.task.title,
            instructions: &input.task.instructions,
        },
        acceptance_criteria: &input.acceptance_criteria,
        policy: GoProposalPolicy {
            adapter: GoProposalAdapterPolicy {
                access: "read_only",
                kind: "omp_audit",
                status: "future",
            },
            authority: "deterministic",
            budget: GoProposalBudgetPolicy {
                dimensions: &input.policy.budget.dimensions,
                status: "future",
            },
            model_role: "advisory_only",
        },
    }
}

fn append_revision_input(input: &AppendProposalRevisionInputBody) -> GoProposalRevisionInput<'_> {
    GoProposalRevisionInput {
        task: GoProposalTask {
            title: &input.task.title,
            instructions: &input.task.instructions,
        },
        acceptance_criteria: &input.acceptance_criteria,
        policy: GoProposalPolicy {
            adapter: GoProposalAdapterPolicy {
                access: "read_only",
                kind: "omp_audit",
                status: "future",
            },
            authority: "deterministic",
            budget: GoProposalBudgetPolicy {
                dimensions: &input.policy.budget.dimensions,
                status: "future",
            },
            model_role: "advisory_only",
        },
    }
}

#[derive(Clone, Debug, Deserialize)]
struct JsonRun {
    id: String,
    project_id: String,
    workstream_id: String,
    state: String,
    worker_pid: i32,
    supervisor_pid: i32,
    committed_offset: i64,
}

impl From<JsonRun> for Run {
    fn from(run: JsonRun) -> Self {
        Self {
            id: run.id,
            state: run.state,
            diagnostics: RunDiagnostics {
                project_id: run.project_id,
                workstream_id: run.workstream_id,
                worker_pid: run.worker_pid.into(),
                supervisor_pid: run.supervisor_pid.into(),
                committed_offset: run.committed_offset,
            },
        }
    }
}

#[derive(Clone, Debug, Deserialize)]
struct EventDto {
    seq: i64,
    #[serde(rename = "type")]
    event_type: String,
    payload: serde_json::Value,
}

impl TryFrom<EventDto> for Event {
    type Error = serde_json::Error;

    fn try_from(event: EventDto) -> Result<Self, Self::Error> {
        Ok(Self {
            payload: serde_json::from_value(event.payload)?,
            seq: event.seq,
            event_type: event.event_type,
        })
    }
}

struct BridgeState {
    backend: Mutex<Backend>,
}

struct Backend {
    paths: DaemonPaths,
    token: String,
    spawned_daemon: Option<std::process::Child>,
}

impl Backend {
    fn new(paths: DaemonPaths) -> Result<Self, BridgeError> {
        let token = load_or_create_token(&paths.token_path())?;
        Ok(Self {
            paths,
            token,
            spawned_daemon: None,
        })
    }

    fn request<'a>(&self, request: GoRequest<'a>) -> Result<GoResponse, BridgeError> {
        let mut stream =
            UnixStream::connect(&self.paths.socket).map_err(BridgeError::SocketConnect)?;
        stream.set_read_timeout(Some(API_TIMEOUT))?;
        stream.set_write_timeout(Some(API_TIMEOUT))?;
        serde_json::to_writer(&mut stream, &request)?;
        stream.write_all(b"\n")?;
        stream.flush()?;
        let response: GoResponse = serde_json::from_reader(stream)?;
        if !response.ok {
            return Err(BridgeError::DaemonRejected(
                response.error.unwrap_or_default(),
            ));
        }
        Ok(response)
    }

    fn ping(&self) -> Result<(), BridgeError> {
        self.request(GoRequest::new("ping", &self.token))
            .map(|_| ())
    }

    fn ensure_daemon(&mut self) -> Result<(), BridgeError> {
        match self.ping() {
            Ok(()) => return Ok(()),
            Err(error) if is_credible_stale_socket_failure(&error) => {}
            Err(error) => return Err(error),
        }
        remove_known_stale_socket(&self.paths.runtime_dir, &self.paths.socket)?;
        self.spawn_daemon()?;
        let deadline = Instant::now() + DAEMON_START_TIMEOUT;
        while Instant::now() < deadline {
            if self.ping().is_ok() {
                return Ok(());
            }
            thread::sleep(Duration::from_millis(50));
        }
        Err(BridgeError::DaemonUnavailable)
    }

    fn daemon_health(&mut self) -> Result<Health, BridgeError> {
        self.ensure_daemon()?;
        Ok(Health { online: true })
    }

    fn spawn_daemon(&mut self) -> Result<(), BridgeError> {
        for binary in [
            &self.paths.daemon_binary,
            &self.paths.supervisor_binary,
            &self.paths.fakeworker_binary,
        ] {
            if !binary.is_file() {
                return Err(BridgeError::MissingBinary);
            }
        }
        let child = Command::new(&self.paths.daemon_binary)
            .arg("-store")
            .arg(self.paths.store_path())
            .arg("-socket")
            .arg(&self.paths.socket)
            .arg("-supervisor-bin")
            .arg(&self.paths.supervisor_binary)
            .arg("-data-dir")
            .arg(&self.paths.data_socket_alias)
            .arg("-token")
            .arg(&self.token)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()?;
        self.spawned_daemon = Some(child);
        Ok(())
    }

    fn bootstrap(&mut self) -> Result<Bootstrap, BridgeError> {
        self.ensure_daemon()?;
        let root = self.paths.repository_root.to_string_lossy();
        let mut create_project = GoRequest::new("create-project", &self.token);
        create_project.id = Some(DEFAULT_PROJECT_ID);
        create_project.name = Some(DEFAULT_PROJECT_NAME);
        create_project.root = Some(root.as_ref());
        self.accept_existing(create_project)?;

        let mut create_workstream = GoRequest::new("create-workstream", &self.token);
        create_workstream.id = Some(DEFAULT_WORKSTREAM_ID);
        create_workstream.project_id = Some(DEFAULT_PROJECT_ID);
        create_workstream.name = Some(DEFAULT_WORKSTREAM_NAME);
        self.accept_existing(create_workstream)?;

        Ok(Bootstrap {
            project: Project {
                id: DEFAULT_PROJECT_ID.into(),
                name: DEFAULT_PROJECT_NAME.into(),
                root: root.into_owned(),
            },
            workstream: Workstream {
                id: DEFAULT_WORKSTREAM_ID.into(),
                name: DEFAULT_WORKSTREAM_NAME.into(),
                project_id: DEFAULT_PROJECT_ID.into(),
            },
        })
    }

    fn accept_existing<'a>(&self, request: GoRequest<'a>) -> Result<(), BridgeError> {
        accept_existing_response(self.request(request))
    }

    fn list_runs(&mut self) -> Result<Vec<Run>, BridgeError> {
        self.ensure_daemon()?;
        let mut request = GoRequest::new("list-runs", &self.token);
        request.project_id = Some(DEFAULT_PROJECT_ID);
        request.workstream_id = Some(DEFAULT_WORKSTREAM_ID);
        Ok(self
            .request(request)?
            .runs
            .into_iter()
            .map(Run::from)
            .collect())
    }

    fn launch_fixture(&mut self) -> Result<Run, BridgeError> {
        self.ensure_daemon()?;
        let run_id = fixture_run_id();
        let worker_env = fixture_worker_env();
        let worker_args = Vec::new();
        let worker_path = self.paths.fakeworker_binary.to_string_lossy();
        let mut request = GoRequest::new("launch-run", &self.token);
        request.id = Some(&run_id);
        request.project_id = Some(DEFAULT_PROJECT_ID);
        request.workstream_id = Some(DEFAULT_WORKSTREAM_ID);
        request.worker_path = Some(worker_path.as_ref());
        request.worker_args = Some(&worker_args);
        request.worker_env = Some(&worker_env);
        self.request(request)?
            .run
            .map(Run::from)
            .ok_or(BridgeError::Protocol)
    }

    fn get_run(&mut self, run_id: &str) -> Result<Run, BridgeError> {
        self.ensure_daemon()?;
        let mut request = GoRequest::new("get-run", &self.token);
        request.id = Some(run_id);
        self.request(request)?
            .run
            .map(Run::from)
            .ok_or(BridgeError::Protocol)
    }

    fn list_events(&mut self, run_id: &str, after_seq: i64) -> Result<Vec<Event>, BridgeError> {
        self.ensure_daemon()?;
        let mut request = GoRequest::new("list-events", &self.token);
        request.id = Some(run_id);
        request.after_seq = Some(after_seq);
        self.request(request)?
            .events
            .into_iter()
            .map(Event::try_from)
            .collect::<Result<Vec<_>, _>>()
            .map_err(BridgeError::from)
    }

    fn cancel_run(&mut self, run_id: &str) -> Result<Cancel, BridgeError> {
        self.ensure_daemon()?;
        let mut request = GoRequest::new("cancel-run", &self.token);
        request.id = Some(run_id);
        let response = self.request(request)?;
        Ok(Cancel {
            accepted: response.accepted,
            state: response.state.ok_or(BridgeError::Protocol)?,
        })
    }

    fn create_proposal(
        &mut self,
        input: CreateProposalInput,
    ) -> Result<ProposalMutation, BridgeError> {
        self.ensure_daemon()?;
        let mut request = GoRequest::new("create-proposal", &self.token);
        request.proposal = Some(create_proposal_request(&input));
        decode_proposal_result(self.request(request)?.proposal_mutation)
    }

    fn list_proposals(&mut self, input: ListProposalsInput) -> Result<ProposalList, BridgeError> {
        self.ensure_daemon()?;
        let mut proposal = GoProposalRequest::empty();
        proposal.project_id = Some(&input.project_id);
        proposal.workstream_id = Some(&input.workstream_id);
        let mut request = GoRequest::new("list-proposals", &self.token);
        request.proposal = Some(proposal);
        Ok(ProposalList {
            proposals: decode_proposal_result(self.request(request)?.proposals)?,
        })
    }

    fn get_proposal(&mut self, input: GetProposalInput) -> Result<ProposalDetail, BridgeError> {
        self.ensure_daemon()?;
        let mut proposal = GoProposalRequest::empty();
        proposal.proposal_id = Some(&input.proposal_id);
        let mut request = GoRequest::new("get-proposal", &self.token);
        request.proposal = Some(proposal);
        decode_proposal_result(self.request(request)?.proposal_detail)
    }

    fn list_proposal_activity(
        &mut self,
        input: ListProposalActivityInput,
    ) -> Result<ProposalActivityList, BridgeError> {
        self.ensure_daemon()?;
        let mut proposal = GoProposalRequest::empty();
        proposal.proposal_id = Some(&input.proposal_id);
        let mut request = GoRequest::new("list-proposal-activity", &self.token);
        request.proposal = Some(proposal);
        Ok(ProposalActivityList {
            activity: decode_proposal_result(self.request(request)?.proposal_activity)?,
        })
    }

    fn append_proposal_revision(
        &mut self,
        input: AppendProposalRevisionInput,
    ) -> Result<ProposalMutation, BridgeError> {
        self.ensure_daemon()?;
        let mut request = GoRequest::new("append-proposal-revision", &self.token);
        request.proposal = Some(append_proposal_request(&input));
        decode_proposal_result(self.request(request)?.proposal_mutation)
    }

    fn decide_proposal_approval(
        &mut self,
        input: DecideProposalApprovalInput,
    ) -> Result<ProposalMutation, BridgeError> {
        self.ensure_daemon()?;
        let mut request = GoRequest::new("decide-proposal-approval", &self.token);
        request.proposal = Some(decision_proposal_request(&input));
        decode_proposal_result(self.request(request)?.proposal_mutation)
    }

    fn withdraw_proposal(
        &mut self,
        input: WithdrawProposalInput,
    ) -> Result<ProposalMutation, BridgeError> {
        self.ensure_daemon()?;
        let mut proposal = GoProposalRequest::empty();
        proposal.idempotency_key = Some(&input.idempotency_key);
        proposal.proposal_id = Some(&input.proposal_id);
        let mut request = GoRequest::new("withdraw-proposal", &self.token);
        request.proposal = Some(proposal);
        decode_proposal_result(self.request(request)?.proposal_mutation)
    }

    #[cfg(test)]
    fn shutdown_for_test(&mut self) {
        if let Some(mut child) = self.spawned_daemon.take() {
            let _ = child.kill();
            let _ = child.wait();
        }
        let _ = remove_known_stale_socket(&self.paths.runtime_dir, &self.paths.socket);
    }
}

fn decode_proposal_result<T: DeserializeOwned>(
    value: Option<serde_json::Value>,
) -> Result<T, BridgeError> {
    serde_json::from_value(value.ok_or(BridgeError::Protocol)?).map_err(BridgeError::from)
}

fn accept_existing_response(response: Result<GoResponse, BridgeError>) -> Result<(), BridgeError> {
    match response {
        Ok(_) => Ok(()),
        Err(BridgeError::DaemonRejected(error)) if is_bootstrap_duplicate_error(&error) => Ok(()),
        Err(error) => Err(error),
    }
}

fn is_bootstrap_duplicate_error(error: &str) -> bool {
    error.contains("UNIQUE constraint failed: projects.id")
        || error.contains("UNIQUE constraint failed: workstreams.id")
}

fn remove_known_stale_socket(runtime_dir: &Path, socket: &Path) -> Result<(), BridgeError> {
    if socket.parent() != Some(runtime_dir) {
        return Err(BridgeError::UnexpectedSocketEndpoint);
    }
    ensure_private_runtime_dir(runtime_dir)?;
    match fs::symlink_metadata(socket) {
        Ok(metadata) if metadata.file_type().is_socket() => {
            fs::remove_file(socket)?;
            Ok(())
        }
        Ok(_) => Err(BridgeError::UnexpectedSocketEndpoint),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(error) => Err(error.into()),
    }
}

#[cfg(debug_assertions)]
fn fixture_worker_env() -> Vec<String> {
    vec![
        "ANANKE_FW_EVENTS=6".to_owned(),
        "ANANKE_FW_EXIT_DELAY_MS=30000".to_owned(),
    ]
}

#[cfg(not(debug_assertions))]
fn fixture_worker_env() -> Vec<String> {
    vec![
        "ANANKE_FW_EVENTS=6".to_owned(),
        "ANANKE_FW_DELAY_MS=250".to_owned(),
        "ANANKE_FW_EXIT_DELAY_MS=750".to_owned(),
    ]
}

fn fixture_run_id() -> String {
    let milliseconds = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis();
    format!("fixture-{milliseconds}")
}

fn use_backend<T>(
    state: State<'_, BridgeState>,
    operation: impl FnOnce(&mut Backend) -> Result<T, BridgeError>,
) -> Result<T, String> {
    let mut backend = state
        .backend
        .lock()
        .map_err(|_| "The Ananke desktop bridge is unavailable.".to_owned())?;
    operation(&mut backend).map_err(|error| error.public_message())
}

#[tauri::command]
fn bootstrap(state: State<'_, BridgeState>) -> Result<Bootstrap, String> {
    use_backend(state, Backend::bootstrap)
}

#[tauri::command]
fn daemon_health(state: State<'_, BridgeState>) -> Result<Health, String> {
    use_backend(state, Backend::daemon_health)
}

#[tauri::command]
fn list_runs(state: State<'_, BridgeState>) -> Result<Vec<Run>, String> {
    use_backend(state, Backend::list_runs)
}

#[tauri::command]
fn launch_fixture(state: State<'_, BridgeState>) -> Result<Run, String> {
    use_backend(state, Backend::launch_fixture)
}

#[tauri::command]
fn get_run(state: State<'_, BridgeState>, run_id: String) -> Result<Run, String> {
    use_backend(state, |backend| backend.get_run(&run_id))
}

#[tauri::command]
fn list_events(
    state: State<'_, BridgeState>,
    run_id: String,
    after_seq: i64,
) -> Result<Vec<Event>, String> {
    use_backend(state, |backend| backend.list_events(&run_id, after_seq))
}

#[tauri::command]
fn cancel_run(state: State<'_, BridgeState>, run_id: String) -> Result<Cancel, String> {
    use_backend(state, |backend| backend.cancel_run(&run_id))
}

#[tauri::command]
fn create_proposal(
    state: State<'_, BridgeState>,
    input: CreateProposalInput,
) -> Result<ProposalMutation, String> {
    use_backend(state, |backend| backend.create_proposal(input))
}

#[tauri::command]
fn list_proposals(
    state: State<'_, BridgeState>,
    input: ListProposalsInput,
) -> Result<ProposalList, String> {
    use_backend(state, |backend| backend.list_proposals(input))
}

#[tauri::command]
fn get_proposal(
    state: State<'_, BridgeState>,
    input: GetProposalInput,
) -> Result<ProposalDetail, String> {
    use_backend(state, |backend| backend.get_proposal(input))
}

#[tauri::command]
fn list_proposal_activity(
    state: State<'_, BridgeState>,
    input: ListProposalActivityInput,
) -> Result<ProposalActivityList, String> {
    use_backend(state, |backend| backend.list_proposal_activity(input))
}

#[tauri::command]
fn append_proposal_revision(
    state: State<'_, BridgeState>,
    input: AppendProposalRevisionInput,
) -> Result<ProposalMutation, String> {
    use_backend(state, |backend| backend.append_proposal_revision(input))
}

#[tauri::command]
fn decide_proposal_approval(
    state: State<'_, BridgeState>,
    input: DecideProposalApprovalInput,
) -> Result<ProposalMutation, String> {
    use_backend(state, |backend| backend.decide_proposal_approval(input))
}

#[tauri::command]
fn withdraw_proposal(
    state: State<'_, BridgeState>,
    input: WithdrawProposalInput,
) -> Result<ProposalMutation, String> {
    use_backend(state, |backend| backend.withdraw_proposal(input))
}

pub fn run() {
    tauri::Builder::default()
        .setup(|app| {
            let paths = DaemonPaths::from_app(app.handle())?;
            let backend = Backend::new(paths)?;
            app.manage(BridgeState {
                backend: Mutex::new(backend),
            });
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            bootstrap,
            daemon_health,
            list_runs,
            launch_fixture,
            get_run,
            list_events,
            cancel_run,
            create_proposal,
            list_proposals,
            get_proposal,
            list_proposal_activity,
            append_proposal_revision,
            decide_proposal_approval,
            withdraw_proposal
        ])
        .run(tauri::generate_context!())
        .expect("error while running Ananke desktop application");
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::ffi::OsStrExt;
    use std::os::unix::fs::MetadataExt;
    use std::os::unix::net::UnixListener;
    use std::sync::atomic::{AtomicU64, Ordering};

    static TEST_COUNTER: AtomicU64 = AtomicU64::new(0);

    fn test_nonce() -> String {
        let counter = TEST_COUNTER.fetch_add(1, Ordering::Relaxed);
        format!("{}-{counter}", std::process::id())
    }

    struct TestEnvironment {
        root: PathBuf,
    }

    impl Drop for TestEnvironment {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.root);
        }
    }

    fn new_test_environment(label: &str) -> TestEnvironment {
        let root = PathBuf::from(format!("/tmp/ananke-gui-{label}-{}", test_nonce()));
        ensure_private_runtime_dir(&root).expect("create private test runtime directory");
        TestEnvironment { root }
    }

    #[test]
    fn creates_and_validates_private_runtime_directory() {
        let environment = new_test_environment("runtime");
        let metadata =
            fs::symlink_metadata(&environment.root).expect("read private runtime metadata");
        assert!(metadata.file_type().is_dir());
        assert!(!metadata.file_type().is_symlink());
        assert_eq!(metadata.permissions().mode() & 0o777, 0o700);
        assert_eq!(metadata.uid(), effective_uid());
        ensure_private_runtime_dir(&environment.root)
            .expect("revalidate private runtime directory");
    }

    #[test]
    fn private_runtime_directory_rejects_symlink_and_untrusted_stale_endpoint() {
        let environment = new_test_environment("runtime-reject");
        let target = environment.root.join("target");
        fs::create_dir(&target).expect("create symlink target");
        let link = environment.root.join("link");
        symlink(&target, &link).expect("create runtime symlink");
        assert!(matches!(
            ensure_private_runtime_dir(&link),
            Err(BridgeError::UnsafeRuntimeDirectory)
        ));

        let outside = PathBuf::from(format!("/tmp/ananke-gui-untrusted-{}.sock", test_nonce()));
        let listener = UnixListener::bind(&outside).expect("create untrusted socket");
        drop(listener);
        let err = remove_known_stale_socket(&environment.root, &outside)
            .expect_err("must reject a socket outside the private runtime directory");
        assert!(matches!(err, BridgeError::UnexpectedSocketEndpoint));
        assert!(outside.exists(), "must not unlink an untrusted endpoint");
        fs::remove_file(outside).expect("remove untrusted test socket");
    }

    #[test]
    fn removes_only_socket_endpoints_in_private_runtime_directory() {
        let environment = new_test_environment("stale");
        let socket = environment.root.join("daemon.sock");
        let listener = UnixListener::bind(&socket).expect("create stale daemon socket");
        drop(listener);
        remove_known_stale_socket(&environment.root, &socket)
            .expect("remove validated stale socket");
        assert!(!socket.exists());

        let file = environment.root.join("not-a-socket");
        fs::write(&file, "not a socket").expect("create non-socket endpoint");
        assert!(matches!(
            remove_known_stale_socket(&environment.root, &file),
            Err(BridgeError::UnexpectedSocketEndpoint)
        ));
        assert!(file.exists(), "must not unlink a non-socket endpoint");
    }

    #[test]
    fn ensure_daemon_preserves_live_rejecting_socket() {
        let environment = new_test_environment("live-rejection");
        let paths = DaemonPaths::from_parts(
            environment.root.join("app-data"),
            environment.root.join("project-root"),
            environment.root.join("missing-sidecars"),
            environment.root.clone(),
        )
        .expect("construct controlled bridge paths");
        let mut backend = Backend::new(paths.clone()).expect("construct bridge backend");
        let listener = UnixListener::bind(&paths.socket).expect("bind rejecting live daemon");
        let server = thread::spawn(move || {
            let (mut stream, _) = listener.accept().expect("accept bridge ping");
            loop {
                let mut byte = [0_u8; 1];
                stream.read_exact(&mut byte).expect("read bridge ping");
                if byte == [b'\n'] {
                    break;
                }
            }
            stream
                .write_all(b"{\"ok\":false,\"error\":\"rejected\"}\n")
                .expect("reject bridge ping");
        });

        let error = backend
            .ensure_daemon()
            .expect_err("live daemon rejection must not trigger stale recovery");
        server.join().expect("join rejecting live daemon");

        assert!(matches!(error, BridgeError::DaemonRejected(_)));
        assert!(
            paths.socket.exists(),
            "must not unlink a live daemon socket"
        );
        assert!(
            backend.spawned_daemon.is_none(),
            "must not spawn a second daemon"
        );
    }

    #[test]
    fn decodes_canonical_api_response_without_secret_fields() {
        let response: GoResponse = serde_json::from_str(
            r#"{"ok":true,"runs":[{"id":"run-a","project_id":"ananke","workstream_id":"main","state":"running","worker_pid":12,"supervisor_pid":11,"committed_offset":42}],"events":[{"seq":1,"type":"message","payload":{"text":"event 1"}}]}"#,
        )
        .expect("decode canonical daemon response");
        assert!(response.ok);
        assert_eq!(response.error, None);
        assert_eq!(response.runs[0].id, "run-a");
        assert_eq!(response.runs[0].state, "running");
        assert_eq!(response.events[0].event_type, "message");
        assert_eq!(response.events[0].payload["text"], "event 1");
    }

    #[test]
    fn generated_public_models_decode_golden_json() {
        let fixture: serde_json::Value = serde_json::from_str(include_str!(
            "../../contracts/fixtures/renderer-public-golden.json"
        ))
        .expect("decode public golden fixture");

        let bootstrap: Bootstrap = serde_json::from_value(fixture["bootstrap"].clone())
            .expect("generated Bootstrap decodes public golden JSON");
        let run: Run = serde_json::from_value(fixture["run"].clone())
            .expect("generated Run decodes public golden JSON");
        let events: Vec<Event> = serde_json::from_value(fixture["events"].clone())
            .expect("generated Event decodes public golden JSON");
        let cancel: Cancel = serde_json::from_value(fixture["cancel"].clone())
            .expect("generated Cancel decodes public golden JSON");
        let health: Health = serde_json::from_value(fixture["health"].clone())
            .expect("generated Health decodes public golden JSON");

        assert_eq!(
            serde_json::to_value(bootstrap).unwrap(),
            fixture["bootstrap"]
        );
        assert_eq!(serde_json::to_value(run).unwrap(), fixture["run"]);
        assert_eq!(serde_json::to_value(events).unwrap(), fixture["events"]);
        assert_eq!(serde_json::to_value(cancel).unwrap(), fixture["cancel"]);
        assert_eq!(serde_json::to_value(health).unwrap(), fixture["health"]);
    }

    #[test]
    fn bootstrap_duplicate_rejection_is_idempotent_but_storage_rejection_fails() {
        let duplicate = BridgeError::DaemonRejected(
            "constraint failed: UNIQUE constraint failed: projects.id (1555)".into(),
        );
        accept_existing_response(Err(duplicate))
            .expect("accept SQLite duplicate bootstrap response");

        let rejection: GoResponse =
            serde_json::from_str(r#"{"ok":false,"error":"database is locked"}"#)
                .expect("decode daemon storage rejection");
        let error = accept_existing_response(Err(BridgeError::DaemonRejected(
            rejection.error.expect("retain daemon rejection detail"),
        )))
        .expect_err("surface non-duplicate daemon rejection");
        assert_eq!(error.public_message(), "The daemon rejected this request.");
        assert!(matches!(
            error,
            BridgeError::DaemonRejected(message) if message == "database is locked"
        ));
    }

    #[test]
    fn release_project_root_is_private_runtime_data_not_builder_checkout() {
        let environment = new_test_environment("release-root");
        let app_data = environment.root.join("app-data");
        let builder_checkout = PathBuf::from("/Users/builder/checkout/ananke");
        let root = select_project_root(&app_data, &builder_checkout, false)
            .expect("select release project root");
        assert_eq!(root, app_data.join("project-root"));
        assert_ne!(root, builder_checkout);
        let metadata = fs::metadata(root).expect("read release project root metadata");
        assert_eq!(metadata.permissions().mode() & 0o777, 0o700);
    }

    #[test]
    fn serializes_authenticated_launch_request() {
        let token = "a".repeat(64);
        let run_id = "fixture-test".to_owned();
        let worker_path = "/tmp/ananke-fakeworker".to_owned();
        let worker_env = vec!["ANANKE_FW_EVENTS=2".to_owned()];
        let worker_args = Vec::new();
        let mut request = GoRequest::new("launch-run", &token);
        request.id = Some(&run_id);
        request.project_id = Some(DEFAULT_PROJECT_ID);
        request.workstream_id = Some(DEFAULT_WORKSTREAM_ID);
        request.worker_path = Some(&worker_path);
        request.worker_args = Some(&worker_args);
        request.worker_env = Some(&worker_env);
        let value = serde_json::to_value(request).expect("serialize launch request");
        assert_eq!(value["cmd"], "launch-run");
        assert_eq!(value["project_id"], DEFAULT_PROJECT_ID);
        assert_eq!(value["worker_env"][0], "ANANKE_FW_EVENTS=2");
        assert_eq!(value["token"].as_str().map(str::len), Some(64));
    }

    #[test]
    fn fixture_worker_env_scopes_cancellable_lifetime_to_debug_builds() {
        #[cfg(debug_assertions)]
        assert_eq!(
            fixture_worker_env(),
            vec![
                "ANANKE_FW_EVENTS=6".to_owned(),
                "ANANKE_FW_EXIT_DELAY_MS=30000".to_owned(),
            ]
        );

        #[cfg(not(debug_assertions))]
        assert_eq!(
            fixture_worker_env(),
            vec![
                "ANANKE_FW_EVENTS=6".to_owned(),
                "ANANKE_FW_DELAY_MS=250".to_owned(),
                "ANANKE_FW_EXIT_DELAY_MS=750".to_owned(),
            ]
        );
    }

    #[test]
    fn daemon_health_public_health_wire_json_is_frozen() {
        let mut test = new_test_backend();
        let health: generated::renderer_public_health::Health = test
            .backend
            .daemon_health()
            .expect("get public daemon health through bridge");

        assert_eq!(
            serde_json::to_value(health).expect("serialize public daemon-health result"),
            serde_json::json!({"online": true})
        );
    }

    #[test]
    fn cancel_run_public_cancel_wire_json_is_frozen() {
        let mut test = new_test_backend();
        test.backend
            .bootstrap()
            .expect("bootstrap before cancelling public run");
        let launched = test
            .backend
            .launch_fixture()
            .expect("launch known public run through bridge");
        let running = wait_for_state(&mut test.backend, &launched.id, "running");
        assert_eq!(running.state, "running");
        let cancellation: generated::renderer_public_cancel::Cancel = test
            .backend
            .cancel_run(&launched.id)
            .expect("cancel public run through bridge");

        assert_eq!(
            serde_json::to_value(cancellation).expect("serialize public cancel-run result"),
            serde_json::json!({"accepted": true, "state": "cancelling"})
        );
        let cancelled = wait_for_state(&mut test.backend, &launched.id, "cancelled");
        assert_eq!(cancelled.state, "cancelled");
    }

    #[test]
    fn bootstrap_public_wire_json_is_frozen() {
        let mut test = new_test_backend();
        let project_root = test
            .backend
            .paths
            .repository_root
            .to_string_lossy()
            .into_owned();
        let bootstrap: generated::renderer_public_bootstrap::Bootstrap = test
            .backend
            .bootstrap()
            .expect("bootstrap through the public bridge");

        assert_eq!(
            serde_json::to_value(&bootstrap).expect("serialize public bootstrap result"),
            serde_json::json!({
                "project": {
                    "id": DEFAULT_PROJECT_ID,
                    "name": DEFAULT_PROJECT_NAME,
                    "root": project_root,
                },
                "workstream": {
                    "id": DEFAULT_WORKSTREAM_ID,
                    "project_id": DEFAULT_PROJECT_ID,
                    "name": DEFAULT_WORKSTREAM_NAME,
                },
            })
        );
    }

    #[test]
    fn list_runs_public_wire_json_is_frozen() {
        let mut test = new_test_backend();
        test.backend
            .bootstrap()
            .expect("bootstrap before listing public runs");
        let launched = test
            .backend
            .launch_fixture()
            .expect("launch known public run through bridge");
        let runs: Vec<generated::renderer_public_run::Run> = test
            .backend
            .list_runs()
            .expect("list public runs through bridge");
        let run = runs
            .into_iter()
            .find(|run| run.id == launched.id)
            .expect("list response contains launched public run");

        assert_eq!(
            serde_json::to_value(run).expect("serialize public list-runs result"),
            serde_json::json!({
                "id": launched.id,
                "state": launched.state,
                "diagnostics": {
                    "project_id": DEFAULT_PROJECT_ID,
                    "workstream_id": DEFAULT_WORKSTREAM_ID,
                    "worker_pid": launched.diagnostics.worker_pid,
                    "supervisor_pid": launched.diagnostics.supervisor_pid,
                    "committed_offset": launched.diagnostics.committed_offset,
                },
            })
        );
    }

    #[test]
    fn launch_fixture_public_run_wire_json_is_frozen() {
        let mut test = new_test_backend();
        test.backend
            .bootstrap()
            .expect("bootstrap before launching public run");
        let launched: Run = test
            .backend
            .launch_fixture()
            .expect("launch public run through bridge");

        assert_eq!(
            serde_json::to_value(&launched).expect("serialize public launch result"),
            serde_json::json!({
                "id": launched.id,
                "state": launched.state,
                "diagnostics": {
                    "project_id": DEFAULT_PROJECT_ID,
                    "workstream_id": DEFAULT_WORKSTREAM_ID,
                    "worker_pid": launched.diagnostics.worker_pid,
                    "supervisor_pid": launched.diagnostics.supervisor_pid,
                    "committed_offset": launched.diagnostics.committed_offset,
                },
            })
        );
    }

    #[test]
    fn get_run_public_run_wire_json_is_frozen() {
        let mut test = new_test_backend();
        test.backend
            .bootstrap()
            .expect("bootstrap before getting public run");
        let launched = test
            .backend
            .launch_fixture()
            .expect("launch known run through bridge");
        let run: Run = test
            .backend
            .get_run(&launched.id)
            .expect("get public run through bridge");

        assert_eq!(
            serde_json::to_value(&run).expect("serialize public get-run result"),
            serde_json::json!({
                "id": launched.id,
                "state": launched.state,
                "diagnostics": {
                    "project_id": DEFAULT_PROJECT_ID,
                    "workstream_id": DEFAULT_WORKSTREAM_ID,
                    "worker_pid": launched.diagnostics.worker_pid,
                    "supervisor_pid": launched.diagnostics.supervisor_pid,
                    "committed_offset": launched.diagnostics.committed_offset,
                },
            })
        );
    }

    #[test]
    fn generated_event_requires_present_non_null_payload() {
        for payload in [
            serde_json::json!({"label": "object", "nested": [1, true]}),
            serde_json::json!(["item", 2, false]),
            serde_json::json!("text value"),
            serde_json::json!(42.5),
            serde_json::json!(true),
        ] {
            let public_json = serde_json::json!({
                "seq": 1,
                "type": "valid",
                "payload": payload,
            });
            let event: generated::renderer_public_event::Event =
                serde_json::from_value(public_json.clone())
                    .expect("generated Event deserializes every valid non-null JSON payload kind");
            assert_eq!(
                serde_json::to_value(event).expect("serialize generated Event"),
                public_json,
            );
        }

        for malformed_json in [
            serde_json::json!({"seq": 1, "type": "missing-payload"}),
            serde_json::json!({"seq": 1, "type": "null-payload", "payload": null}),
        ] {
            assert!(
                serde_json::from_value::<generated::renderer_public_event::Event>(
                    malformed_json.clone()
                )
                .is_err(),
                "generated Event must reject a missing or null payload: {malformed_json}",
            );
        }
    }

    #[test]
    fn list_events_public_wire_json_preserves_arbitrary_payloads() {
        let mut test = new_test_backend();
        test.backend
            .bootstrap()
            .expect("bootstrap before listing public events");

        let fixture_path = test.environment.root.join("event-payload-fixture.sh");
        fs::write(
            &fixture_path,
            r#"#!/bin/sh
cat > "$ANANKE_FW_TRANSCRIPT" <<'EOF'
{"type":"object","payload":{"label":"object","nested":[1,true]}}
{"type":"array","payload":["item",2,false]}
{"type":"string","payload":"text value"}
{"type":"number","payload":42.5}
{"type":"boolean","payload":true}
EOF
"#,
        )
        .expect("write event payload fixture");
        let mut permissions = fs::metadata(&fixture_path)
            .expect("read event payload fixture metadata")
            .permissions();
        permissions.set_mode(0o700);
        fs::set_permissions(&fixture_path, permissions)
            .expect("make event payload fixture executable");

        let run_id = format!("event-payload-{}", test_nonce());
        let worker_path = fixture_path.to_string_lossy().into_owned();
        let worker_args = Vec::new();
        let worker_env = Vec::new();
        let mut request = GoRequest::new("launch-run", &test.backend.token);
        request.id = Some(&run_id);
        request.project_id = Some(DEFAULT_PROJECT_ID);
        request.workstream_id = Some(DEFAULT_WORKSTREAM_ID);
        request.worker_path = Some(&worker_path);
        request.worker_args = Some(&worker_args);
        request.worker_env = Some(&worker_env);
        let response = test
            .backend
            .request(request)
            .expect("launch event payload fixture through bridge");
        assert!(response.ok);

        let deadline = Instant::now() + Duration::from_secs(10);
        let events: Vec<generated::renderer_public_event::Event> = loop {
            let events = test
                .backend
                .list_events(&run_id, 0)
                .expect("list event payload fixture through bridge");
            if events.len() == 5 {
                break events;
            }
            assert!(
                Instant::now() < deadline,
                "event payload fixture did not produce five events"
            );
            thread::sleep(Duration::from_millis(50));
        };

        assert_eq!(
            serde_json::to_value(events).expect("serialize public list-events result"),
            serde_json::json!([
                {
                    "seq": 1,
                    "type": "object",
                    "payload": {"label": "object", "nested": [1, true]},
                },
                {
                    "seq": 2,
                    "type": "array",
                    "payload": ["item", 2, false],
                },
                {
                    "seq": 3,
                    "type": "string",
                    "payload": "text value",
                },
                {
                    "seq": 4,
                    "type": "number",
                    "payload": 42.5,
                },
                {
                    "seq": 5,
                    "type": "boolean",
                    "payload": true,
                },
            ])
        );
    }

    struct TestBackend {
        backend: Backend,
        environment: TestEnvironment,
    }

    impl Drop for TestBackend {
        fn drop(&mut self) {
            self.backend.shutdown_for_test();
            let _ = fs::remove_dir_all(&self.environment.root);
        }
    }

    fn new_test_backend() -> TestBackend {
        let environment = new_test_environment("bridge");
        let repository_root = development_repository_root();
        let source_binaries = repository_root.join(".ananke/bin");
        let sidecars = environment.root.join("sidecars");
        fs::create_dir(&sidecars).expect("create controlled sidecar directory");
        for binary in ["ananke", "ananke-supervisor", "ananke-fakeworker"] {
            let source = source_binaries.join(binary);
            assert!(
                source.is_file(),
                "test setup requires built GUI sidecar {binary}; run npm run build:go"
            );
            let destination = sidecars.join(binary);
            fs::copy(&source, &destination).expect("copy required test sidecar");
            fs::set_permissions(
                &destination,
                fs::metadata(&source)
                    .expect("read sidecar metadata")
                    .permissions(),
            )
            .expect("preserve sidecar executable mode");
        }
        let paths = DaemonPaths::from_parts(
            environment.root.join("app-data"),
            environment.root.join("project-root"),
            sidecars,
            environment.root.clone(),
        )
        .expect("construct bridge paths");
        let backend = Backend::new(paths).expect("construct bridge backend");
        TestBackend {
            backend,
            environment,
        }
    }

    fn wait_for_events(backend: &mut Backend, run_id: &str) -> Vec<Event> {
        let deadline = Instant::now() + Duration::from_secs(10);
        while Instant::now() < deadline {
            let events = backend
                .list_events(run_id, 0)
                .expect("list canonical events through bridge");
            if !events.is_empty() {
                return events;
            }
            thread::sleep(Duration::from_millis(50));
        }
        panic!("run {run_id} did not produce canonical events");
    }

    fn wait_for_state(backend: &mut Backend, run_id: &str, wanted: &str) -> Run {
        let deadline = Instant::now() + Duration::from_secs(15);
        while Instant::now() < deadline {
            let run = backend.get_run(run_id).expect("get run through bridge");
            if run.state == wanted {
                return run;
            }
            thread::sleep(Duration::from_millis(50));
        }
        panic!("run {run_id} did not reach {wanted}");
    }

    #[test]
    fn bridge_bootstrap_launches_lists_events_cancels_and_reconnects() {
        let mut first = new_test_backend();
        let bootstrap = first
            .backend
            .bootstrap()
            .expect("bootstrap through public bridge method");
        assert_eq!(bootstrap.project.id, DEFAULT_PROJECT_ID);
        assert_eq!(bootstrap.workstream.id, DEFAULT_WORKSTREAM_ID);
        assert!(
            first
                .backend
                .list_runs()
                .expect("list initial runs through bridge")
                .is_empty()
        );

        let launched = first
            .backend
            .launch_fixture()
            .expect("launch fixture through bridge");
        let supervisor_socket = first
            .backend
            .paths
            .data_socket_alias
            .join(&launched.id)
            .join("supervisor.sock");
        assert!(
            supervisor_socket.as_os_str().as_bytes().len() < 104,
            "fixture supervisor socket must fit Darwin's Unix-domain socket limit"
        );
        let events = wait_for_events(&mut first.backend, &launched.id);
        assert!(events.iter().all(|event| {
            serde_json::to_value(event)
                .expect("serialize generated Event")
                .get("payload")
                .is_some_and(|payload| !payload.is_null())
        }));
        let cancellation = first
            .backend
            .cancel_run(&launched.id)
            .expect("cancel fixture through bridge");
        assert!(cancellation.accepted);
        let cancelled = wait_for_state(&mut first.backend, &launched.id, "cancelled");
        assert_eq!(cancelled.state, "cancelled");

        let mut second = Backend::new(first.backend.paths.clone())
            .expect("reconnect with persisted token and runtime endpoint");
        let runs = second
            .list_runs()
            .expect("list persisted runs through reconnecting bridge");
        assert!(
            runs.iter()
                .any(|run| run.id == launched.id && run.state == "cancelled")
        );
        let token_mode = fs::metadata(first.backend.paths.token_path())
            .expect("read persisted token")
            .permissions()
            .mode()
            & 0o777;
        assert_eq!(token_mode, 0o600);
        let app_data_mode = fs::metadata(&first.backend.paths.app_data_dir)
            .expect("read private app-data directory")
            .permissions()
            .mode()
            & 0o777;
        assert_eq!(app_data_mode, 0o700);
    }

    #[test]
    fn bridge_proposals_serialize_public_wire_replay_conflicts_and_reconnect() {
        let mut first = new_test_backend();
        let project_id = "project_p1c";
        let workstream_id = "workstream_p1c";

        let create_a = proposal_create_input(
            "proposal_bridge_create_a",
            project_id,
            workstream_id,
            "Approve through bridge",
        );
        let created_a = first
            .backend
            .create_proposal(create_a.clone())
            .expect("create proposal through the real bridge");
        assert_public_object(
            &serde_json::to_value(&created_a).expect("serialize created proposal mutation"),
            &["approval_id", "proposal_id", "revision", "revision_hash"],
        );
        let replayed_a = first
            .backend
            .create_proposal(create_a.clone())
            .expect("replay created proposal through the real bridge");
        assert_eq!(
            replayed_a, created_a,
            "same proposal input must replay its durable identity"
        );
        let conflict = first
            .backend
            .create_proposal(proposal_create_input(
                "proposal_bridge_create_a",
                project_id,
                workstream_id,
                "Conflicting bridge proposal",
            ))
            .expect_err("same key with a different body must conflict");
        assert_sanitized_proposal_error(&conflict);

        let initial_detail = first
            .backend
            .get_proposal(
                generated::renderer_public_proposal_get_input::GetProposalInput {
                    proposal_id: created_a.proposal_id.clone(),
                },
            )
            .expect("get created proposal through the real bridge");
        let initial_detail_wire =
            serde_json::to_value(&initial_detail).expect("serialize proposal detail");
        assert_public_object(
            &initial_detail_wire,
            &["approval", "lifecycle", "proposal", "revision"],
        );
        assert_eq!(initial_detail_wire["proposal"]["state"], "open");
        assert_eq!(initial_detail_wire["revision"]["revision"], 1);

        let appended_a = first
            .backend
            .append_proposal_revision(proposal_append_input(
                "proposal_bridge_append_a",
                &created_a,
                "Approve appended bridge revision",
            ))
            .expect("append proposal revision through the real bridge");
        let approved_a = first
            .backend
            .decide_proposal_approval(proposal_decision_input(
                "proposal_bridge_approve_a",
                &appended_a,
                "approved",
                "Meets the reviewed bridge contract.",
            ))
            .expect("approve appended proposal through the real bridge");
        assert_eq!(approved_a, appended_a);

        let create_b = first
            .backend
            .create_proposal(proposal_create_input(
                "proposal_bridge_create_b",
                project_id,
                workstream_id,
                "Reject through bridge",
            ))
            .expect("create rejection proposal through the real bridge");
        first
            .backend
            .decide_proposal_approval(proposal_decision_input(
                "proposal_bridge_reject_b",
                &create_b,
                "rejected",
                "Needs a narrower reviewed task.",
            ))
            .expect("reject proposal through the real bridge");

        let create_c = first
            .backend
            .create_proposal(proposal_create_input(
                "proposal_bridge_create_c",
                project_id,
                workstream_id,
                "Withdraw through bridge",
            ))
            .expect("create withdrawal proposal through the real bridge");
        let withdrawn_c = first
            .backend
            .withdraw_proposal(
                generated::renderer_public_proposal_withdraw_input::WithdrawProposalInput {
                    idempotency_key: "proposal_bridge_withdraw_c".into(),
                    proposal_id: create_c.proposal_id.clone(),
                },
            )
            .expect("withdraw proposal through the real bridge");
        assert_eq!(withdrawn_c, create_c);

        let list_input = generated::renderer_public_proposal_list_input::ListProposalsInput {
            project_id: project_id.into(),
            workstream_id: workstream_id.into(),
        };
        let listed = first
            .backend
            .list_proposals(list_input.clone())
            .expect("list proposal summaries through the real bridge");
        let listed_wire = serde_json::to_value(&listed).expect("serialize proposal list");
        assert_public_object(&listed_wire, &["proposals"]);
        let proposals = listed_wire["proposals"]
            .as_array()
            .expect("proposal summaries array");
        assert_eq!(proposals.len(), 3);
        assert_eq!(proposals[0]["proposal_id"], created_a.proposal_id);
        assert_eq!(proposals[1]["proposal_id"], create_b.proposal_id);
        assert_eq!(proposals[2]["proposal_id"], create_c.proposal_id);
        for proposal in proposals {
            assert_public_object(
                proposal,
                &[
                    "created_at",
                    "created_by",
                    "current_revision",
                    "current_revision_hash",
                    "project_id",
                    "proposal_id",
                    "state",
                    "workstream_id",
                ],
            );
        }

        let activity_a = first
            .backend
            .list_proposal_activity(generated::renderer_public_proposal_activity_list_input::ListProposalActivityInput {
                proposal_id: created_a.proposal_id.clone(),
            })
            .expect("list approved proposal activity through the real bridge");
        let activity_a_wire =
            serde_json::to_value(&activity_a).expect("serialize approved proposal activity");
        assert_public_object(&activity_a_wire, &["activity"]);
        assert_eq!(
            activity_a_wire["activity"]
                .as_array()
                .expect("approved activity array")
                .iter()
                .map(|activity| activity["operation"].as_str().expect("activity operation"))
                .collect::<Vec<_>>(),
            vec!["create_proposal", "append_revision", "decide_approval"],
        );

        let rejected_detail = first
            .backend
            .get_proposal(
                generated::renderer_public_proposal_get_input::GetProposalInput {
                    proposal_id: create_b.proposal_id.clone(),
                },
            )
            .expect("get rejected proposal through the real bridge");
        assert_eq!(
            serde_json::to_value(rejected_detail).expect("serialize rejected detail")["approval"]["state"],
            "rejected"
        );
        let withdrawn_detail = first
            .backend
            .get_proposal(
                generated::renderer_public_proposal_get_input::GetProposalInput {
                    proposal_id: create_c.proposal_id.clone(),
                },
            )
            .expect("get withdrawn proposal through the real bridge");
        assert_eq!(
            serde_json::to_value(withdrawn_detail).expect("serialize withdrawn detail")["proposal"]
                ["state"],
            "withdrawn"
        );

        let not_found = first
            .backend
            .get_proposal(
                generated::renderer_public_proposal_get_input::GetProposalInput {
                    proposal_id: "proposal_missing".into(),
                },
            )
            .expect_err("missing proposal must fail through the bridge");
        assert_sanitized_proposal_error(&not_found);

        let missing_activity = first
            .backend
            .list_proposal_activity(
                generated::renderer_public_proposal_activity_list_input::ListProposalActivityInput {
                    proposal_id: "proposal_missing".into(),
                },
            )
            .expect_err("missing proposal activity must not expose an empty public activity list");
        assert!(matches!(
            &missing_activity,
            BridgeError::DaemonRejected(message) if message == "proposal not found"
        ));
        assert_sanitized_proposal_error(&missing_activity);

        let mut reconnected =
            Backend::new(first.backend.paths.clone()).expect("construct reconnecting bridge");
        let reconnected_wire = serde_json::to_value(
            reconnected
                .list_proposals(list_input)
                .expect("list persisted proposal summaries through reconnecting bridge"),
        )
        .expect("serialize reconnected proposal list");
        assert_eq!(reconnected_wire, listed_wire);
    }

    fn proposal_create_input(
        idempotency_key: &str,
        project_id: &str,
        workstream_id: &str,
        title: &str,
    ) -> generated::renderer_public_proposal_create_input::CreateProposalInput {
        serde_json::from_value(serde_json::json!({
            "idempotency_key": idempotency_key,
            "project_id": project_id,
            "workstream_id": workstream_id,
            "revision_input": proposal_revision_input(title),
        }))
        .expect("decode generated create proposal input")
    }

    fn proposal_append_input(
        idempotency_key: &str,
        mutation: &generated::renderer_public_proposal_mutation::ProposalMutation,
        title: &str,
    ) -> generated::renderer_public_proposal_append_input::AppendProposalRevisionInput {
        serde_json::from_value(serde_json::json!({
            "idempotency_key": idempotency_key,
            "proposal_id": mutation.proposal_id,
            "expected_current_revision": mutation.revision,
            "expected_current_revision_hash": mutation.revision_hash,
            "revision_input": proposal_revision_input(title),
        }))
        .expect("decode generated append proposal input")
    }

    fn proposal_decision_input(
        idempotency_key: &str,
        mutation: &generated::renderer_public_proposal_mutation::ProposalMutation,
        decision: &str,
        reason: &str,
    ) -> generated::renderer_public_proposal_decision_input::DecideProposalApprovalInput {
        serde_json::from_value(serde_json::json!({
            "idempotency_key": idempotency_key,
            "approval_id": mutation.approval_id,
            "proposal_id": mutation.proposal_id,
            "revision": mutation.revision,
            "revision_hash": mutation.revision_hash,
            "decision": decision,
            "reason": reason,
        }))
        .expect("decode generated approval decision input")
    }

    fn proposal_revision_input(title: &str) -> serde_json::Value {
        serde_json::json!({
            "task": {
                "title": title,
                "instructions": "Preserve the frozen proposal boundary without execution."
            },
            "acceptance_criteria": ["Use only durable proposal records."],
            "policy": {
                "adapter": {"access": "read_only", "kind": "omp_audit", "status": "future"},
                "authority": "deterministic",
                "budget": {"dimensions": ["deadline", "attempt_cap"], "status": "future"},
                "model_role": "advisory_only"
            }
        })
    }

    fn assert_public_object(value: &serde_json::Value, expected_keys: &[&str]) {
        let object = value.as_object().expect("generated public wire object");
        let mut actual = object.keys().map(String::as_str).collect::<Vec<_>>();
        actual.sort_unstable();
        let mut expected = expected_keys.to_vec();
        expected.sort_unstable();
        assert_eq!(actual, expected, "public wire fields must be exact");
        for forbidden in [
            "cmd", "token", "ok", "error", "socket", "path", "root", "worker", "pid",
        ] {
            assert!(
                !object.contains_key(forbidden),
                "public wire object leaked private field {forbidden}: {value}"
            );
        }
    }

    fn assert_sanitized_proposal_error(error: &BridgeError) {
        let message = error.public_message();
        assert_eq!(message, "The daemon rejected this request.");
        for private in [
            "idempotency_conflict",
            "proposal not found",
            "cmd",
            "token",
            "socket",
            "path",
        ] {
            assert!(
                !message.contains(private),
                "public error leaked private daemon data {private}: {message}"
            );
        }
    }
}
