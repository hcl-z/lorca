package app.lorca.core

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.ConnectivityManager
import android.net.Network
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import org.json.JSONObject

/// Keeps the process, and with it the core's sync socket, running while the app is in the
/// background, when the user turns on Keep Connected. What a sync lands that would have come as
/// a push (a reply, a failure, a question) the core emits as a `notice` (`push::local`), and the
/// service posts it at once, with no push service in between. A foreground service must show
/// its own notification; it sits on a quiet channel of its own.
class SyncService : Service() {
  private var networkCallback: ConnectivityManager.NetworkCallback? = null

  private val onEvent: (String) -> Unit = { json -> notice(json) }

  override fun onBind(intent: Intent?): IBinder? = null

  override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    if (!enabled(this)) {
      stopSelf()
      return START_NOT_STICKY
    }
    foreground()
    if (!running) {
      running = true
      CoreHolder.addListener(onEvent)
      // Restarted by the system with no app on screen: the service starts the core itself.
      val core = try { CoreHolder.core ?: CoreHolder.startHere(this) } catch (_: Throwable) { null }
      core?.setLocalNotices(true)
      core?.wake()
      watchNetwork()
    }
    // The system starts the service again after it had to stop it.
    return START_STICKY
  }

  override fun onDestroy() {
    running = false
    CoreHolder.removeListener(onEvent)
    CoreHolder.core?.setLocalNotices(false)
    networkCallback?.let { callback -> runCatching { getSystemService(ConnectivityManager::class.java).unregisterNetworkCallback(callback) } }
    networkCallback = null
    super.onDestroy()
  }

  private fun foreground() {
    val manager = getSystemService(NotificationManager::class.java)
    manager.createNotificationChannel(NotificationChannel(CHANNEL, "Keep connected", NotificationManager.IMPORTANCE_MIN).apply { setShowBadge(false) })
    val open = packageManager.getLaunchIntentForPackage(packageName)
    val tap = open?.let { PendingIntent.getActivity(this, 0, it, PendingIntent.FLAG_IMMUTABLE) }
    val notification: Notification = NotificationCompat.Builder(this, CHANNEL)
      .setSmallIcon(applicationInfo.icon)
      .setContentTitle(applicationInfo.loadLabel(packageManager).toString())
      .setContentText(if (development()) "Connected to your Devices (dev)" else "Connected to your Devices")
      .setOngoing(true)
      .setSilent(true)
      .setPriority(NotificationCompat.PRIORITY_MIN)
      .setContentIntent(tap)
      .build()
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
      startForeground(NOTIFICATION_ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
    } else {
      startForeground(NOTIFICATION_ID, notification)
    }
  }

  /// A network that comes back (Wi-Fi to mobile data, out of a lift) reconnects at once rather
  /// than after the socket's silence runs out.
  private fun watchNetwork() {
    if (networkCallback != null) return
    val callback = object : ConnectivityManager.NetworkCallback() {
      override fun onAvailable(network: Network) {
        CoreHolder.core?.wake()
      }
    }
    if (runCatching { getSystemService(ConnectivityManager::class.java).registerDefaultNetworkCallback(callback) }.isSuccess) networkCallback = callback
  }

  private fun notice(json: String) {
    val frame = runCatching { JSONObject(json) }.getOrNull() ?: return
    if (frame.optString("event") != "notice") return
    val data = frame.optJSONObject("data") ?: return
    val chatId = data.optString("chat_id")
    // The user is looking at that chat: the reply is arriving in front of them.
    if (PushService.inFront && chatId == PushService.openChat) return
    val subtitle = if (data.isNull("subtitle")) null else data.optString("subtitle")
    Notices.post(this, data.optString("title"), subtitle, data.optString("body"), chatId, data.optString("message_id").hashCode())
  }

  private fun development() = packageName == "app.lorca.dev"

  companion object {
    private const val CHANNEL = "keep-connected"
    private const val NOTIFICATION_ID = 7
    private const val PREFS = "lorca-sync"
    private const val ENABLED = "keep_connected"

    @Volatile var running = false
      private set

    /// Off until the user turns it on in Settings.
    fun enabled(context: Context) = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getBoolean(ENABLED, false)

    fun setEnabled(context: Context, on: Boolean) {
      context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putBoolean(ENABLED, on).apply()
      if (on) start(context) else context.stopService(Intent(context, SyncService::class.java))
    }

    /// Starts the service when it is on; called while the app is in front, which Android 12 and
    /// later require of a foreground service's start.
    fun start(context: Context) {
      if (enabled(context)) runCatching { context.startForegroundService(Intent(context, SyncService::class.java)) }
    }
  }
}
