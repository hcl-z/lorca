package app.lorca.core

import android.annotation.SuppressLint
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.PowerManager
import android.provider.Settings

/// What lets the background service keep running: the system's battery optimization, and on
/// phones from makers that stop background apps on their own, the maker's switches for running
/// in the background and starting by itself, which no API reports. Their pages are the makers'
/// own activities and move between versions, so each is tried only if it resolves, and the
/// app's details page, which links to the same switches, is the fallback.
object BackgroundAccess {
  fun batteryExempt(context: Context): Boolean =
    context.getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(context.packageName)

  /// The system's prompt to let this app ignore battery optimization.
  @SuppressLint("BatteryLife")
  fun requestBatteryExemption(context: Context) {
    val prompt = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:${context.packageName}"))
    if (!open(context, prompt)) open(context, Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS))
  }

  /// Whether this phone's maker has background switches of its own.
  fun hasMakerSettings(): Boolean = pages().isNotEmpty()

  /// The maker's page for running in the background, else the app's details page.
  fun openMakerSettings(context: Context) {
    for (component in pages()) {
      if (open(context, Intent().setComponent(component))) return
    }
    open(context, Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:${context.packageName}")))
  }

  private fun pages(): List<ComponentName> {
    val maker = "${Build.MANUFACTURER} ${Build.BRAND}".lowercase()
    fun page(pkg: String, cls: String) = ComponentName(pkg, cls)
    return when {
      "xiaomi" in maker || "redmi" in maker || "poco" in maker -> listOf(
        page("com.miui.securitycenter", "com.miui.permcenter.autostart.AutoStartManagementActivity"),
      )
      "vivo" in maker || "iqoo" in maker -> listOf(
        page("com.vivo.abe", "com.vivo.applicationbehaviorengine.ui.ExcessivePowerManagerActivity"),
        page("com.vivo.permissionmanager", "com.vivo.permissionmanager.activity.BgStartUpManagerActivity"),
      )
      "huawei" in maker -> listOf(
        page("com.huawei.systemmanager", "com.huawei.systemmanager.startupmgr.ui.StartupNormalAppListActivity"),
      )
      "honor" in maker -> listOf(
        page("com.hihonor.systemmanager", "com.hihonor.systemmanager.startupmgr.ui.StartupNormalAppListActivity"),
      )
      "oppo" in maker || "oneplus" in maker || "realme" in maker -> listOf(
        page("com.coloros.safecenter", "com.coloros.safecenter.startupapp.StartupAppListActivity"),
      )
      else -> emptyList()
    }
  }

  /// Tried rather than resolved first: package visibility hides other makers' apps from
  /// `resolveActivity`. A page that is missing or not exported throws, and the next one goes.
  private fun open(context: Context, intent: Intent): Boolean {
    intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    return runCatching { context.startActivity(intent) }.isSuccess
  }
}
