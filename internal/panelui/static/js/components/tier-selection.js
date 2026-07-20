export function isCurrentTierAvailable(tiers, currentTierIdentifier) {
  if (!currentTierIdentifier) {
    return false;
  }
  return tiers.some((tier) => tier.id === currentTierIdentifier);
}
