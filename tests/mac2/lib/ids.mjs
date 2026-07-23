import mac2SelectorContract from "../../../contracts/mac2-accessibility.json" with { type: "json" };

export const WEB_ACCESSIBILITY_SELECTOR_ATTRIBUTE = mac2SelectorContract.selectorAttribute;

export const ACCESSIBILITY_IDS = Object.freeze({ ...mac2SelectorContract.selectors });

export const PREFLIGHT_ACCESSIBILITY_IDS = Object.freeze([
  ACCESSIBILITY_IDS.bootstrapState,
  ACCESSIBILITY_IDS.daemonHealth,
  ACCESSIBILITY_IDS.refresh,
  ACCESSIBILITY_IDS.runList,
  ACCESSIBILITY_IDS.launchFixture,
  ACCESSIBILITY_IDS.grillReview,
]);
