package app.lorca.core

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.core.app.NotificationCompat

/// A reply, failure, or question as a notification: who replied, where, and the first words,
/// tagged with its chat. A tap opens the chat through this build's app link. Posted for an FCM
/// push (PushService) and for what the background service's sync lands (SyncService).
object Notices {
  private const val CHANNEL = "replies"

  /// `chatId` null: a push this phone could not open, which says only "New reply".
  fun post(context: Context, title: String?, subtitle: String?, body: String?, chatId: String?, id: Int) {
    val manager = context.getSystemService(NotificationManager::class.java)
    manager.createNotificationChannel(NotificationChannel(CHANNEL, "Replies", NotificationManager.IMPORTANCE_HIGH))
    val scheme = if (context.packageName == "app.lorca.dev") "lorca-dev" else "lorca"
    val open = Intent(Intent.ACTION_VIEW, Uri.parse(if (chatId != null) "$scheme://chat/$chatId" else "$scheme://")).setPackage(context.packageName)
    val tap = PendingIntent.getActivity(context, chatId.hashCode(), open, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
    val heading = when {
      title == null -> context.applicationInfo.loadLabel(context.packageManager).toString()
      subtitle != null -> "$title · $subtitle"
      else -> title
    }
    val text = body ?: "New reply"
    val notification = NotificationCompat.Builder(context, CHANNEL)
      .setSmallIcon(context.applicationInfo.icon)
      .setContentTitle(heading)
      .setContentText(text)
      .setStyle(NotificationCompat.BigTextStyle().bigText(text))
      .setGroup(chatId)
      .setAutoCancel(true)
      .setContentIntent(tap)
      .build()
    manager.notify(chatId ?: "lorca", id, notification)
  }

  /// Takes down what was posted for a chat; each notification is tagged with its chat.
  fun clear(context: Context, chatId: String) {
    val manager = context.getSystemService(NotificationManager::class.java)
    manager.activeNotifications.filter { it.tag == chatId }.forEach { manager.cancel(it.tag, it.id) }
  }
}
