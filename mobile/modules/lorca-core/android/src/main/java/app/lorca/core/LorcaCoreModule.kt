package app.lorca.core

import expo.modules.kotlin.functions.Coroutine
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import uniffi.lorca_mobile.Core

/// The Rust core behind the phone: started once with the app's folder and the phone's facts,
/// then one request at a time and a stream of events. The core belongs to the process
/// (CoreHolder), which the background service shares.
class LorcaCoreModule : Module() {
  private val forward: (String) -> Unit = { json -> sendEvent("event", mapOf("json" to json)) }

  private val core: Core? get() = CoreHolder.core

  override fun definition() = ModuleDefinition {
    Name("LorcaCore")

    Events("event", "update")

    OnCreate {
      Updater.onStatus = { status -> sendEvent("update", status) }
      CoreHolder.addListener(forward)
    }

    OnDestroy {
      CoreHolder.removeListener(forward)
    }

    // Off the JS thread, so the first frame does not wait for the account to load. The service
    // may have started the core already; this one is then the same.
    AsyncFunction("start").Coroutine { home: String, name: String, os: String, osVersion: String, model: String ->
      withContext(Dispatchers.IO) {
        val started = CoreHolder.start(home, name, os, osVersion, model)
        if (SyncService.running) started.setLocalNotices(true)
        appContext.reactContext?.let { SyncService.start(it) }
      }
    }

    // Blocks until the core answers. On the IO pool, never the JS thread and never the
    // module's single queue thread: a request that waits (pair.accept, up to ten minutes)
    // must not hold every other request behind it.
    AsyncFunction("request").Coroutine { method: String, params: String ->
      val running = core ?: throw IllegalStateException("The Lorca core has not been started")
      withContext(Dispatchers.IO) { running.request(method, params) }
    }

    Function("wake") {
      core?.wake()
    }

    // The chat on screen, or null. While the app is in front PushService posts nothing for
    // it, and whatever it posted for it has now been seen.
    Function("setOpenChat") { chatId: String? ->
      PushService.openChat = chatId
      if (chatId != null && PushService.inFront) clearPosted(chatId)
    }

    // Keep Connected: what it is set to and what lets it run, for Settings.
    Function("backgroundStatus") {
      val context = appContext.reactContext ?: return@Function null
      mapOf(
        "enabled" to SyncService.enabled(context),
        "running" to SyncService.running,
        "batteryExempt" to BackgroundAccess.batteryExempt(context),
        "makerSettings" to BackgroundAccess.hasMakerSettings(),
      )
    }

    Function("setKeepConnected") { on: Boolean ->
      appContext.reactContext?.let { SyncService.setEnabled(it, on) }
      if (!on) core?.setLocalNotices(false)
    }

    Function("requestBatteryExemption") {
      appContext.reactContext?.let { BackgroundAccess.requestBatteryExemption(it) }
    }

    Function("openMakerSettings") {
      appContext.reactContext?.let { BackgroundAccess.openMakerSettings(it) }
    }

    // A release's APK: downloaded, checked, and handed to the package installer. Resolves once
    // the installer has it; how that ends arrives as "update" events.
    AsyncFunction("installUpdate").Coroutine { url: String, sha256: String, size: Double ->
      val context = appContext.reactContext ?: throw IllegalStateException("The app has no Android context")
      val total = size.toLong()
      withContext(Dispatchers.IO) {
        val apk = Updater.download(context, url, sha256, total) { received ->
          sendEvent("update", mapOf("state" to "downloading", "received" to received.toDouble(), "total" to total.toDouble()))
        }
        sendEvent("update", mapOf("state" to "installing"))
        Updater.install(context, apk)
      }
    }

    OnActivityEntersForeground {
      PushService.inFront = true
      PushService.openChat?.let { clearPosted(it) }
      appContext.currentActivity?.let { Updater.showConfirmation(it) }
      // A service the system stopped comes back while the app is in front, which it requires.
      appContext.reactContext?.let { SyncService.start(it) }
    }

    OnActivityEntersBackground {
      PushService.inFront = false
    }
  }

  private fun clearPosted(chatId: String) {
    appContext.reactContext?.let { PushService.clear(it, chatId) }
  }
}
