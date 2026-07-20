const ACTIVE_RUN_STATES: Record<string, true> = {
  created: true,
  running: true,
  cancelling: true,
  cleanup_required: true,
  recovery_unknown: true,
};

export const isActiveRunState = (state: string): boolean => ACTIVE_RUN_STATES[state] === true;
