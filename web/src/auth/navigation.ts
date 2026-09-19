import type { UserProfile } from "../api/client";

export function signedInHome(profile?: UserProfile) {
  if (profile?.portal?.isCustomerUser) return "/portal/customer";
  if (
    profile?.portal?.franchise &&
    profile.permissions?.includes("portal.franchise")
  )
    return "/portal/franchise";
  return "/shipments";
}

export function signInDestination(profile: UserProfile, from?: unknown) {
  if (profile.mustChangePassword) return "/change-password";
  const home = signedInHome(profile);
  if (
    typeof from !== "string" ||
    !from.startsWith("/") ||
    from.startsWith("//") ||
    from.includes("\\") ||
    Array.from(from).some((character) => character.charCodeAt(0) < 32)
  )
    return home;
  if (["/", "/login", "/change-password", "/shipments"].includes(from))
    return home;
  if (
    profile.portal?.isCustomerUser &&
    from !== "/portal/customer" &&
    !from.startsWith("/portal/customer/")
  )
    return home;
  if (
    !profile.portal?.isCustomerUser &&
    (from === "/portal/customer" || from.startsWith("/portal/customer/"))
  )
    return home;
  if (
    home !== "/portal/franchise" &&
    (from === "/portal/franchise" || from.startsWith("/portal/franchise/"))
  )
    return home;
  return from;
}
