#!/bin/zsh
set -euo pipefail

readonly group_name="_ananke_repair"
readonly group_id="62000"
readonly -a user_names=("_ananke_repair_1" "_ananke_repair_2" "_ananke_repair_3" "_ananke_repair_4")
readonly -a user_ids=("62001" "62002" "62003" "62004")
readonly user_shell="/usr/bin/false"
readonly user_home="/var/empty"

fail() {
  print -u2 -- "P6_RUNTIME_UID_POOL=FAIL: $1"
  exit 1
}

read_attribute() {
  local record="$1"
  local attribute="$2"
  dscl . -read "$record" "$attribute" 2>/dev/null |
    sed -n -e "s/^${attribute}: //p" -e "s/^dsAttrTypeNative:${attribute}: //p"
}

search_record_names() {
  local record_type="$1"
  local attribute="$2"
  local value="$3"
  dscl . -search "$record_type" "$attribute" "$value" 2>/dev/null |
    awk -v attribute="$attribute" '$2 == attribute { print $1 }'
}

check_group() {
  local named gid_collision_names
  named="$(read_attribute "/Groups/${group_name}" PrimaryGroupID || true)"
  gid_collision_names="$(search_record_names /Groups PrimaryGroupID "$group_id" || true)"
  if [[ -n "$named" && "$named" != "$group_id" ]]; then
    fail "group ${group_name} exists with GID ${named}, expected ${group_id}"
  fi
  if [[ -n "$gid_collision_names" && "$gid_collision_names" != "$group_name" ]]; then
    fail "GID ${group_id} is already assigned to unexpected record(s): ${gid_collision_names}"
  fi
}

check_user() {
  local name="$1" uid="$2"
  local existing_uid existing_gid existing_shell existing_home existing_hidden existing_password uid_collision_names
  existing_uid="$(read_attribute "/Users/${name}" UniqueID || true)"
  uid_collision_names="$(search_record_names /Users UniqueID "$uid" || true)"
  if [[ -n "$existing_uid" && "$existing_uid" != "$uid" ]]; then
    fail "user ${name} exists with UID ${existing_uid}, expected ${uid}"
  fi
  if [[ -n "$uid_collision_names" && "$uid_collision_names" != "$name" ]]; then
    fail "UID ${uid} is already assigned to unexpected record(s): ${uid_collision_names}"
  fi
  if [[ -n "$existing_uid" ]]; then
    existing_gid="$(read_attribute "/Users/${name}" PrimaryGroupID || true)"
    existing_shell="$(read_attribute "/Users/${name}" UserShell || true)"
    existing_home="$(read_attribute "/Users/${name}" NFSHomeDirectory || true)"
    existing_hidden="$(read_attribute "/Users/${name}" IsHidden || true)"
    existing_password="$(read_attribute "/Users/${name}" Password || true)"
    [[ "$existing_gid" == "$group_id" ]] || fail "user ${name} has GID ${existing_gid}, expected ${group_id}"
    [[ "$existing_shell" == "$user_shell" ]] || fail "user ${name} has shell ${existing_shell}, expected ${user_shell}"
    [[ "$existing_home" == "$user_home" ]] || fail "user ${name} has home ${existing_home}, expected ${user_home}"
    [[ "$existing_hidden" == "1" ]] || fail "user ${name} is not hidden"
    [[ "$existing_password" == "*" || "$existing_password" == "********" ]] || fail "user ${name} does not have a locked password marker"
  fi
}

check_pool() {
  [[ -x "$user_shell" ]] || fail "${user_shell} is unavailable"
  [[ -d "$user_home" ]] || fail "${user_home} is unavailable"
  check_group
  local index
  for index in {1..4}; do
    check_user "${user_names[$index]}" "${user_ids[$index]}"
  done
  print -- "P6_RUNTIME_UID_POOL_CHECK=PASS"
}

apply_pool() {
  [[ "$EUID" == "0" ]] || fail "--apply must run under sudo"
  check_pool

  if ! dscl . -read "/Groups/${group_name}" >/dev/null 2>&1; then
    dscl . -create "/Groups/${group_name}"
    dscl . -create "/Groups/${group_name}" PrimaryGroupID "$group_id"
    dscl . -create "/Groups/${group_name}" RealName "Ananke Controlled Repair Runtime"
  fi

  local index name uid
  for index in {1..4}; do
    name="${user_names[$index]}"
    uid="${user_ids[$index]}"
    if ! dscl . -read "/Users/${name}" >/dev/null 2>&1; then
      dscl . -create "/Users/${name}"
      dscl . -create "/Users/${name}" UniqueID "$uid"
      dscl . -create "/Users/${name}" PrimaryGroupID "$group_id"
      dscl . -create "/Users/${name}" RealName "Ananke Controlled Repair Runtime ${index}"
      dscl . -create "/Users/${name}" UserShell "$user_shell"
      dscl . -create "/Users/${name}" NFSHomeDirectory "$user_home"
      dscl . -create "/Users/${name}" IsHidden 1
      dscl . -create "/Users/${name}" Password '*'
    fi
    dseditgroup -o edit -a "$name" -t user "$group_name"
  done

  dscacheutil -flushcache
  check_pool
  for name in "${user_names[@]}"; do
    if dseditgroup -o checkmember -m "$name" admin 2>/dev/null | grep -q 'yes'; then
      fail "${name} unexpectedly belongs to admin"
    fi
  done
  print -- "P6_RUNTIME_UID_POOL_APPLY=PASS"
}

case "${1:-}" in
  --check) check_pool ;;
  --apply) apply_pool ;;
  *) fail "usage: $0 --check | --apply" ;;
esac
