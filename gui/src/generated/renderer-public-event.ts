/**
 * Public result of the Tauri list_events command.
 */
export interface Event {
    /**
     * Arbitrary non-null JSON payload from the daemon event stream.
     */
    payload: unknown[] | boolean | number | { [key: string]: unknown } | string;
    seq:     number;
    type:    string;
}
