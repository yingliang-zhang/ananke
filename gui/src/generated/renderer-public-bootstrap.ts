/**
 * Public result of the Tauri bootstrap command.
 */
export interface Bootstrap {
    project:    Project;
    workstream: Workstream;
}

export interface Project {
    id:   string;
    name: string;
    root: string;
}

export interface Workstream {
    id:         string;
    name:       string;
    project_id: string;
}
