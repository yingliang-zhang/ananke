/**
 * Public result of the Tauri list_runs command.
 */
export interface Run {
    diagnostics: RunDiagnostics;
    id:          string;
    state:       string;
}

export interface RunDiagnostics {
    committed_offset: number;
    project_id:       string;
    supervisor_pid:   number;
    worker_pid:       number;
    workstream_id:    string;
}
