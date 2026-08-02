export async function refreshLoginVerificationBeforeMissingTokenError({
  state,
  turnstileToken,
  reloadAuthenticationSettings,
  renderApplication
}) {
  if (!state.turnstileEnabled || turnstileToken) {
    return true;
  }

  state.authenticationSettingsStatus = "loading";
  state.authenticationSettingsError = "";
  renderApplication();
  await reloadAuthenticationSettings();
  if (state.authenticationSettingsStatus !== "ready") {
    renderApplication();
    return false;
  }
  if (state.turnstileEnabled) {
    state.authError = "请先完成人机验证。";
    renderApplication();
    return false;
  }
  return true;
}
