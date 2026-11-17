// The server stores every valid contact as international Nigerian digits.
window.normalizeNigerianPhone = function (raw) {
  let value = raw.replace(/[ \t\n\r\f\v()-]/g, "");
  if (!/^\+?[0-9]+$/.test(value)) return "";
  if (value.startsWith("+")) {
    if (!value.startsWith("+234")) return "";
    value = value.slice(1);
  }
  if (value.startsWith("00")) value = value.slice(2);
  if (value.startsWith("234")) value = value.slice(3);
  if (value.startsWith("0")) value = value.slice(1);
  return /^[789][0-9]{9}$/.test(value) ? "234" + value : "";
};
