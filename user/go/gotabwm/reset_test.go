// reset_test.go — M216h (#3947): one reset for the whole seat.
//
// gotabwm is package main: the seat's state lives in package globals and
// its guest-coupled calls are function variables tests swap. Every test
// calls resetSeatForTest(t) first; it returns the seat to the state the
// package boots with — every zero-initialized global zeroed, every
// non-zero default (config tables, sentinels, -1 sentinels) and every
// swappable seam restored to its post-init value — and re-applies the
// same reset in t.Cleanup so a swapped seam cannot leak into the next
// test under -shuffle=on.
//
// The lists below are generated from the package's var declarations:
// ZERO-class globals all have zero-value initializers, so zeroing IS the
// boot state; RESTORE-class globals carry a non-zero initializer or are
// func seams, so they get their captured post-init default back (init()
// wraps execApp/closeWin in ceiling.go, which is why capture is lazy).

package main

import (
	"maps"
	"slices"
	"sync"
	"testing"

	"virelai/chords"
	"virelai/theme"
)

func zeroSeatVar[T any](p *T) { *p = *new(T) }

func setSeatVar[T any](p *T, v any) { *p = v.(T) }

var (
	seatResetOnce sync.Once
	seatDefaults  = map[string]any{}
	// Reference-typed defaults are cloned both ways so a test mutating a
	// restored table cannot pollute the snapshot.
	defAgentMilestones []struct{ cmd, name string }
	defSeatChords      map[chords.Chord]string
)

func captureSeatDefaults() {
	seatResetOnce.Do(func() {
		seatDefaults["agentIpcSend"] = agentIpcSend
		seatDefaults["agentListFn"] = agentListFn
		seatDefaults["agentLogFn"] = agentLogFn
		seatDefaults["agentProcName"] = agentProcName
		seatDefaults["agentReadFn"] = agentReadFn
		seatDefaults["agentSecretGet"] = agentSecretGet
		seatDefaults["agentToastFn"] = agentToastFn
		seatDefaults["clipMenuSetTyped"] = clipMenuSetTyped
		seatDefaults["clockZoneLoad"] = clockZoneLoad
		seatDefaults["clockZoneMin"] = clockZoneMin
		seatDefaults["closeWin"] = closeWin
		seatDefaults["configureHostedChrome"] = configureHostedChrome
		seatDefaults["crashLog"] = crashLog
		seatDefaults["curCursorShape"] = curCursorShape
		seatDefaults["cursorShapeNames"] = cursorShapeNames
		seatDefaults["dragFileDelete"] = dragFileDelete
		seatDefaults["dragFileRead"] = dragFileRead
		seatDefaults["dragFileWrite"] = dragFileWrite
		seatDefaults["dragLog"] = dragLog
		seatDefaults["dragOutDelete"] = dragOutDelete
		seatDefaults["dragOutExists"] = dragOutExists
		seatDefaults["dragOutWrite"] = dragOutWrite
		seatDefaults["dragPost"] = dragPost
		seatDefaults["dropListDir"] = dropListDir
		seatDefaults["dropReadFile"] = dropReadFile
		seatDefaults["errDropUnreadable"] = errDropUnreadable
		seatDefaults["errLaunchRead"] = errLaunchRead
		seatDefaults["errRFBInputFlood"] = errRFBInputFlood
		seatDefaults["errRFBSendStalled"] = errRFBSendStalled
		seatDefaults["errRFBSocket"] = errRFBSocket
		seatDefaults["errWindowLimit"] = errWindowLimit
		seatDefaults["execApp"] = execApp
		seatDefaults["focusByIndex"] = focusByIndex
		seatDefaults["focusNotifySource"] = focusNotifySource
		seatDefaults["focusRaise"] = focusRaise
		seatDefaults["focusedCpuTicks"] = focusedCpuTicks
		seatDefaults["forwardContentKey"] = forwardContentKey
		seatDefaults["forwardContentPointer"] = forwardContentPointer
		seatDefaults["gpu3dBroker"] = gpu3dBroker
		seatDefaults["gpu3dDescMap"] = gpu3dDescMap
		seatDefaults["gpu3dLog"] = gpu3dLog
		seatDefaults["gpu3dSend"] = gpu3dSend
		seatDefaults["hostClipGet"] = hostClipGet
		seatDefaults["hostClipSet"] = hostClipSet
		seatDefaults["hostfocusConsume"] = hostfocusConsume
		seatDefaults["hostfocusFocus"] = hostfocusFocus
		seatDefaults["hostintDeleteFile"] = hostintDeleteFile
		seatDefaults["hostintListDir"] = hostintListDir
		seatDefaults["hostintLog"] = hostintLog
		seatDefaults["hostintReadFile"] = hostintReadFile
		seatDefaults["hostintSecretGet"] = hostintSecretGet
		seatDefaults["hostintWriteFile"] = hostintWriteFile
		seatDefaults["hostoutboxDeleteFile"] = hostoutboxDeleteFile
		seatDefaults["hostoutboxListDir"] = hostoutboxListDir
		seatDefaults["hostoutboxListFailed"] = hostoutboxListFailed
		seatDefaults["hostoutboxReadFile"] = hostoutboxReadFile
		seatDefaults["hostprintDeleteFile"] = hostprintDeleteFile
		seatDefaults["hostprintListDir"] = hostprintListDir
		seatDefaults["hostprintListFailed"] = hostprintListFailed
		seatDefaults["hostprintReadFile"] = hostprintReadFile
		seatDefaults["hostspeechDeleteFile"] = hostspeechDeleteFile
		seatDefaults["hostspeechListDir"] = hostspeechListDir
		seatDefaults["hostspeechListFailed"] = hostspeechListFailed
		seatDefaults["hostspeechReadFile"] = hostspeechReadFile
		seatDefaults["hoverPreview"] = hoverPreview
		seatDefaults["hoverPreviewLog"] = hoverPreviewLog
		seatDefaults["kernelTime"] = kernelTime
		seatDefaults["launchDirList"] = launchDirList
		seatDefaults["listProcs"] = listProcs
		seatDefaults["loadSeatRegistry"] = loadSeatRegistry
		seatDefaults["loadSettingsFile"] = loadSettingsFile
		seatDefaults["loadSettingsForBus"] = loadSettingsForBus
		seatDefaults["lockReadFile"] = lockReadFile
		seatDefaults["mintAuthValue"] = mintAuthValue
		seatDefaults["notifyDataRoot"] = notifyDataRoot
		seatDefaults["notifyHistoryPath"] = notifyHistoryPath
		seatDefaults["notifyPlayChime"] = notifyPlayChime
		seatDefaults["notifySettingsLoad"] = notifySettingsLoad
		seatDefaults["openBlurProbe"] = openBlurProbe
		seatDefaults["openFile"] = openFile
		seatDefaults["openInputSink"] = openInputSink
		seatDefaults["openSinkWindow"] = openSinkWindow
		seatDefaults["outboxTickFn"] = outboxTickFn
		seatDefaults["paletteDirList"] = paletteDirList
		seatDefaults["paletteFocus"] = paletteFocus
		seatDefaults["paletteSettings"] = paletteSettings
		seatDefaults["panesDelete"] = panesDelete
		seatDefaults["powerFocus"] = powerFocus
		seatDefaults["powerRows"] = powerRows
		seatDefaults["printTickFn"] = printTickFn
		seatDefaults["procsAlive"] = procsAlive
		seatDefaults["procsCpuTicks"] = procsCpuTicks
		seatDefaults["procsExitStatus"] = procsExitStatus
		seatDefaults["queryWindow"] = queryWindow
		seatDefaults["railCloseHover"] = railCloseHover
		seatDefaults["railDragFrom"] = railDragFrom
		seatDefaults["railHover"] = railHover
		seatDefaults["railMenuFocus"] = railMenuFocus
		seatDefaults["readAppsFile"] = readAppsFile
		seatDefaults["readNotifyFile"] = readNotifyFile
		seatDefaults["readSessionFile"] = readSessionFile
		seatDefaults["readWindowName"] = readWindowName
		seatDefaults["restoreSettingsWM"] = restoreSettingsWM
		seatDefaults["saveNotifyDND"] = saveNotifyDND
		seatDefaults["seatA11yDelete"] = seatA11yDelete
		seatDefaults["seatA11yWrite"] = seatA11yWrite
		seatDefaults["secretAskCommit"] = secretAskCommit
		seatDefaults["secretAskDrop"] = secretAskDrop
		seatDefaults["secretAskList"] = secretAskList
		seatDefaults["secretLog"] = secretLog
		seatDefaults["selFileDelete"] = selFileDelete
		seatDefaults["selFileRead"] = selFileRead
		seatDefaults["selFileWrite"] = selFileWrite
		seatDefaults["selLog"] = selLog
		seatDefaults["selPost"] = selPost
		seatDefaults["sendProgressNotice"] = sendProgressNotice
		seatDefaults["sendSettingsNotice"] = sendSettingsNotice
		seatDefaults["sessionSeq"] = sessionSeq
		seatDefaults["setCursorShape"] = setCursorShape
		seatDefaults["setWindowRect"] = setWindowRect
		seatDefaults["shotList"] = shotList
		seatDefaults["shotLog"] = shotLog
		seatDefaults["shotMkdir"] = shotMkdir
		seatDefaults["shotNow"] = shotNow
		seatDefaults["shotRead"] = shotRead
		seatDefaults["shotWrite"] = shotWrite
		seatDefaults["snapDelete"] = snapDelete
		seatDefaults["snapExists"] = snapExists
		seatDefaults["snapList"] = snapList
		seatDefaults["snapLog"] = snapLog
		seatDefaults["snapMkdir"] = snapMkdir
		seatDefaults["snapRead"] = snapRead
		seatDefaults["snapWrite"] = snapWrite
		seatDefaults["spaceN"] = spaceN
		seatDefaults["spaces"] = spaces
		seatDefaults["speechTickFn"] = speechTickFn
		seatDefaults["stateFileDelete"] = stateFileDelete
		seatDefaults["stateFileRead"] = stateFileRead
		seatDefaults["stateFileWrite"] = stateFileWrite
		seatDefaults["stateLog"] = stateLog
		seatDefaults["stateNoticePost"] = stateNoticePost
		seatDefaults["stateServiceDrain"] = stateServiceDrain
		seatDefaults["stateYield"] = stateYield
		seatDefaults["summonFocus"] = summonFocus
		seatDefaults["surfaceAttach"] = surfaceAttach
		seatDefaults["surfaceName"] = surfaceName
		seatDefaults["vsockCtlRequest"] = vsockCtlRequest
		seatDefaults["wdKill"] = wdKill
		seatDefaults["wdLog"] = wdLog
		seatDefaults["wdPrincipal"] = wdPrincipal
		seatDefaults["wdProcsUsage"] = wdProcsUsage
		seatDefaults["wmctlSetRect"] = wmctlSetRect
		seatDefaults["wmctlSetVisible"] = wmctlSetVisible
		seatDefaults["wqSend"] = wqSend
		seatDefaults["writeHostFile"] = writeHostFile
		seatDefaults["writeNotifyFile"] = writeNotifyFile
		seatDefaults["writeWindowChrome"] = writeWindowChrome
		defAgentMilestones = slices.Clone(agentMilestones)
		defSeatChords = maps.Clone(seatChords)
	})
}

func applySeatReset() {
	zeroSeatVar(&agentActive)
	zeroSeatVar(&agentCaughtUp)
	zeroSeatVar(&agentKey)
	zeroSeatVar(&agentSeen)
	zeroSeatVar(&badgeReported)
	zeroSeatVar(&busyMascotShown)
	zeroSeatVar(&buttonPixels)
	zeroSeatVar(&ceilArmed)
	zeroSeatVar(&ceilBound)
	zeroSeatVar(&ceilRan)
	zeroSeatVar(&ceilSeen)
	zeroSeatVar(&ceilTick)
	zeroSeatVar(&centerSpotShown)
	zeroSeatVar(&chromeLogged)
	zeroSeatVar(&chromeScratch)
	zeroSeatVar(&clipHist)
	zeroSeatVar(&clipHistBuf)
	zeroSeatVar(&clipHistHash)
	zeroSeatVar(&clipHistLen)
	zeroSeatVar(&clipHistN)
	zeroSeatVar(&clipHistSeen)
	zeroSeatVar(&clipMenuList)
	zeroSeatVar(&clipMenuOpen)
	zeroSeatVar(&clipMenuPrior)
	zeroSeatVar(&clipMenuRows)
	zeroSeatVar(&clipMenuSel)
	zeroSeatVar(&clipMenuSink)
	zeroSeatVar(&clockZoneVal)
	zeroSeatVar(&compositeClock)
	zeroSeatVar(&contentDown)
	zeroSeatVar(&crashMascotShown)
	zeroSeatVar(&crashRestartPending)
	zeroSeatVar(&crashRestarted)
	zeroSeatVar(&crashRestartedN)
	zeroSeatVar(&crashSnapSeq)
	zeroSeatVar(&cursorShapeSeen)
	zeroSeatVar(&cycleCpuInit)
	zeroSeatVar(&cycleCpuLast)
	zeroSeatVar(&cycleCpuQuiet)
	zeroSeatVar(&cycleParkedTick)
	zeroSeatVar(&cyclePending)
	zeroSeatVar(&cycleQueue)
	zeroSeatVar(&cycleQueueN)
	zeroSeatVar(&cycleShift)
	zeroSeatVar(&demoMode)
	zeroSeatVar(&dogfoodHosted)
	zeroSeatVar(&drag)
	zeroSeatVar(&dragGhostReceipt)
	zeroSeatVar(&godButtonHover)
	zeroSeatVar(&gpu3dDescBuf)
	zeroSeatVar(&gpu3dRecs)
	zeroSeatVar(&gpu3dRects)
	zeroSeatVar(&hostClipBuf)
	zeroSeatVar(&hostClipHash)
	zeroSeatVar(&hostClipLaneHeld)
	zeroSeatVar(&hostClipLen)
	zeroSeatVar(&hostClipSeen)
	zeroSeatVar(&hostClipSourceHost)
	zeroSeatVar(&hostNotifyPending)
	zeroSeatVar(&hostTicksLeft)
	zeroSeatVar(&hostedApp)
	zeroSeatVar(&hostintEngine)
	zeroSeatVar(&iconByBin)
	zeroSeatVar(&idleAfter)
	zeroSeatVar(&idleMascotShown)
	zeroSeatVar(&idlePresented)
	zeroSeatVar(&idleState)
	zeroSeatVar(&idleWakePending)
	zeroSeatVar(&kernelFocusID)
	zeroSeatVar(&lastActivityTick)
	zeroSeatVar(&lastInputTick)
	zeroSeatVar(&lastRailFocus)
	zeroSeatVar(&lastRailN)
	zeroSeatVar(&lastRailSpace)
	zeroSeatVar(&launch)
	zeroSeatVar(&launchPending)
	zeroSeatVar(&launchPendingN)
	zeroSeatVar(&limitNoticeTick)
	zeroSeatVar(&limitNoticed)
	zeroSeatVar(&lockActive)
	zeroSeatVar(&lockFile)
	zeroSeatVar(&lockMascotShown)
	zeroSeatVar(&lockPass)
	zeroSeatVar(&lockPrior)
	zeroSeatVar(&lockSink)
	zeroSeatVar(&lockSinkShared)
	zeroSeatVar(&menuGesture)
	zeroSeatVar(&menuPixels)
	zeroSeatVar(&notifyCenter)
	zeroSeatVar(&notifyCenterNextID)
	zeroSeatVar(&notifyCenterOpen)
	zeroSeatVar(&notifyCenterReceipt)
	zeroSeatVar(&notifyDND)
	zeroSeatVar(&notifyDropped)
	zeroSeatVar(&notifyHistDirty)
	zeroSeatVar(&notifyHistFailLogged)
	zeroSeatVar(&notifyQueue)
	zeroSeatVar(&notifyReceipt)
	zeroSeatVar(&notifyReduceMotion)
	zeroSeatVar(&notifySoundBytes)
	zeroSeatVar(&notifySoundChime)
	zeroSeatVar(&notifySoundParams)
	zeroSeatVar(&notifySoundPending)
	zeroSeatVar(&notifySoundSkipped)
	zeroSeatVar(&pendingAppStateN)
	zeroSeatVar(&pendingAppStates)
	zeroSeatVar(&pendingComposite)
	zeroSeatVar(&pendingHeartbeat)
	zeroSeatVar(&pendingNavID)
	zeroSeatVar(&pendingNavPath)
	zeroSeatVar(&powerBtnHover)
	zeroSeatVar(&powerBtnLogged)
	zeroSeatVar(&powerMenuOpen)
	zeroSeatVar(&powerPixels)
	zeroSeatVar(&powerSel)
	zeroSeatVar(&powerSink)
	zeroSeatVar(&prevPtrButtons)
	zeroSeatVar(&primSel)
	zeroSeatVar(&primSelSet)
	zeroSeatVar(&railIconsMarked)
	zeroSeatVar(&railMarked)
	zeroSeatVar(&railMenuOpen)
	zeroSeatVar(&railMenuPrior)
	zeroSeatVar(&railMenuSel)
	zeroSeatVar(&railMenuSink)
	zeroSeatVar(&railMenuTab)
	zeroSeatVar(&railReceipt)
	zeroSeatVar(&railSpinMarked)
	zeroSeatVar(&reexecHold)
	zeroSeatVar(&reexecN)
	zeroSeatVar(&reexecPending)
	zeroSeatVar(&reexecSettleArmed)
	zeroSeatVar(&refusalDeduped)
	zeroSeatVar(&refusalSeen)
	zeroSeatVar(&refusalSeenN)
	zeroSeatVar(&reserveWin)
	zeroSeatVar(&restoreWitnessMode)
	zeroSeatVar(&rpcBindings)
	zeroSeatVar(&rpcChallenges)
	zeroSeatVar(&rpcReplyPayload)
	zeroSeatVar(&sashDragNode)
	zeroSeatVar(&sashDragging)
	zeroSeatVar(&seatA11yLast)
	zeroSeatVar(&seatFonts)
	zeroSeatVar(&seatSinks)
	zeroSeatVar(&seatTick)
	zeroSeatVar(&secretAskModal)
	zeroSeatVar(&serviceRPCBuf)
	zeroSeatVar(&sessionDirty)
	zeroSeatVar(&sessionEcho)
	zeroSeatVar(&settingsBusValues)
	zeroSeatVar(&settingsSubscriptions)
	zeroSeatVar(&snapCpuInit)
	zeroSeatVar(&snapCpuLast)
	zeroSeatVar(&snapCpuQuiet)
	zeroSeatVar(&snapParkedTick)
	zeroSeatVar(&snapPending)
	zeroSeatVar(&spaceCur)
	zeroSeatVar(&startSurfaceLogged)
	zeroSeatVar(&stateAdoptPend)
	zeroSeatVar(&stateAskPend)
	zeroSeatVar(&stripClosedOne)
	zeroSeatVar(&stripDone)
	zeroSeatVar(&stripHoldLeft)
	zeroSeatVar(&stripSawTwo)
	zeroSeatVar(&stripStep)
	zeroSeatVar(&surfaceNameBuf)
	zeroSeatVar(&surfaceRecs)
	zeroSeatVar(&surfaceXY)
	zeroSeatVar(&tabs)
	zeroSeatVar(&wdAsked)
	zeroSeatVar(&wdBusy)
	zeroSeatVar(&wdBusyStreak)
	zeroSeatVar(&wdBusyWin)
	zeroSeatVar(&wdEverAsked)
	zeroSeatVar(&wdKillOK)
	zeroSeatVar(&wdKilled)
	zeroSeatVar(&wdLastSweep)
	zeroSeatVar(&wdMiss)
	zeroSeatVar(&wdPrevCpu)
	zeroSeatVar(&wdPrevInit)
	zeroSeatVar(&wdPrevTotal)
	zeroSeatVar(&wdProto)
	zeroSeatVar(&wdSweepInit)
	zeroSeatVar(&wdWedged)
	setSeatVar(&agentIpcSend, seatDefaults["agentIpcSend"])
	setSeatVar(&agentListFn, seatDefaults["agentListFn"])
	setSeatVar(&agentLogFn, seatDefaults["agentLogFn"])
	setSeatVar(&agentProcName, seatDefaults["agentProcName"])
	setSeatVar(&agentReadFn, seatDefaults["agentReadFn"])
	setSeatVar(&agentSecretGet, seatDefaults["agentSecretGet"])
	setSeatVar(&agentToastFn, seatDefaults["agentToastFn"])
	setSeatVar(&clipMenuSetTyped, seatDefaults["clipMenuSetTyped"])
	setSeatVar(&clockZoneLoad, seatDefaults["clockZoneLoad"])
	setSeatVar(&clockZoneMin, seatDefaults["clockZoneMin"])
	setSeatVar(&closeWin, seatDefaults["closeWin"])
	setSeatVar(&configureHostedChrome, seatDefaults["configureHostedChrome"])
	setSeatVar(&crashLog, seatDefaults["crashLog"])
	setSeatVar(&curCursorShape, seatDefaults["curCursorShape"])
	setSeatVar(&cursorShapeNames, seatDefaults["cursorShapeNames"])
	setSeatVar(&dragFileDelete, seatDefaults["dragFileDelete"])
	setSeatVar(&dragFileRead, seatDefaults["dragFileRead"])
	setSeatVar(&dragFileWrite, seatDefaults["dragFileWrite"])
	setSeatVar(&dragLog, seatDefaults["dragLog"])
	setSeatVar(&dragOutDelete, seatDefaults["dragOutDelete"])
	setSeatVar(&dragOutExists, seatDefaults["dragOutExists"])
	setSeatVar(&dragOutWrite, seatDefaults["dragOutWrite"])
	setSeatVar(&dragPost, seatDefaults["dragPost"])
	setSeatVar(&dropListDir, seatDefaults["dropListDir"])
	setSeatVar(&dropReadFile, seatDefaults["dropReadFile"])
	setSeatVar(&errDropUnreadable, seatDefaults["errDropUnreadable"])
	setSeatVar(&errLaunchRead, seatDefaults["errLaunchRead"])
	setSeatVar(&errRFBInputFlood, seatDefaults["errRFBInputFlood"])
	setSeatVar(&errRFBSendStalled, seatDefaults["errRFBSendStalled"])
	setSeatVar(&errRFBSocket, seatDefaults["errRFBSocket"])
	setSeatVar(&errWindowLimit, seatDefaults["errWindowLimit"])
	setSeatVar(&execApp, seatDefaults["execApp"])
	setSeatVar(&focusByIndex, seatDefaults["focusByIndex"])
	setSeatVar(&focusNotifySource, seatDefaults["focusNotifySource"])
	setSeatVar(&focusRaise, seatDefaults["focusRaise"])
	setSeatVar(&focusedCpuTicks, seatDefaults["focusedCpuTicks"])
	setSeatVar(&forwardContentKey, seatDefaults["forwardContentKey"])
	setSeatVar(&forwardContentPointer, seatDefaults["forwardContentPointer"])
	setSeatVar(&gpu3dBroker, seatDefaults["gpu3dBroker"])
	setSeatVar(&gpu3dDescMap, seatDefaults["gpu3dDescMap"])
	setSeatVar(&gpu3dLog, seatDefaults["gpu3dLog"])
	setSeatVar(&gpu3dSend, seatDefaults["gpu3dSend"])
	setSeatVar(&hostClipGet, seatDefaults["hostClipGet"])
	setSeatVar(&hostClipSet, seatDefaults["hostClipSet"])
	setSeatVar(&hostfocusConsume, seatDefaults["hostfocusConsume"])
	setSeatVar(&hostfocusFocus, seatDefaults["hostfocusFocus"])
	setSeatVar(&hostintDeleteFile, seatDefaults["hostintDeleteFile"])
	setSeatVar(&hostintListDir, seatDefaults["hostintListDir"])
	setSeatVar(&hostintLog, seatDefaults["hostintLog"])
	setSeatVar(&hostintReadFile, seatDefaults["hostintReadFile"])
	setSeatVar(&hostintSecretGet, seatDefaults["hostintSecretGet"])
	setSeatVar(&hostintWriteFile, seatDefaults["hostintWriteFile"])
	setSeatVar(&hostoutboxDeleteFile, seatDefaults["hostoutboxDeleteFile"])
	setSeatVar(&hostoutboxListDir, seatDefaults["hostoutboxListDir"])
	setSeatVar(&hostoutboxListFailed, seatDefaults["hostoutboxListFailed"])
	setSeatVar(&hostoutboxReadFile, seatDefaults["hostoutboxReadFile"])
	setSeatVar(&hostprintDeleteFile, seatDefaults["hostprintDeleteFile"])
	setSeatVar(&hostprintListDir, seatDefaults["hostprintListDir"])
	setSeatVar(&hostprintListFailed, seatDefaults["hostprintListFailed"])
	setSeatVar(&hostprintReadFile, seatDefaults["hostprintReadFile"])
	setSeatVar(&hostspeechDeleteFile, seatDefaults["hostspeechDeleteFile"])
	setSeatVar(&hostspeechListDir, seatDefaults["hostspeechListDir"])
	setSeatVar(&hostspeechListFailed, seatDefaults["hostspeechListFailed"])
	setSeatVar(&hostspeechReadFile, seatDefaults["hostspeechReadFile"])
	setSeatVar(&hoverPreview, seatDefaults["hoverPreview"])
	setSeatVar(&hoverPreviewLog, seatDefaults["hoverPreviewLog"])
	setSeatVar(&kernelTime, seatDefaults["kernelTime"])
	setSeatVar(&launchDirList, seatDefaults["launchDirList"])
	setSeatVar(&listProcs, seatDefaults["listProcs"])
	setSeatVar(&loadSeatRegistry, seatDefaults["loadSeatRegistry"])
	setSeatVar(&loadSettingsFile, seatDefaults["loadSettingsFile"])
	setSeatVar(&loadSettingsForBus, seatDefaults["loadSettingsForBus"])
	setSeatVar(&lockReadFile, seatDefaults["lockReadFile"])
	setSeatVar(&mintAuthValue, seatDefaults["mintAuthValue"])
	setSeatVar(&notifyDataRoot, seatDefaults["notifyDataRoot"])
	setSeatVar(&notifyHistoryPath, seatDefaults["notifyHistoryPath"])
	setSeatVar(&notifyPlayChime, seatDefaults["notifyPlayChime"])
	setSeatVar(&notifySettingsLoad, seatDefaults["notifySettingsLoad"])
	setSeatVar(&openBlurProbe, seatDefaults["openBlurProbe"])
	setSeatVar(&openFile, seatDefaults["openFile"])
	setSeatVar(&openInputSink, seatDefaults["openInputSink"])
	setSeatVar(&openSinkWindow, seatDefaults["openSinkWindow"])
	setSeatVar(&outboxTickFn, seatDefaults["outboxTickFn"])
	setSeatVar(&paletteDirList, seatDefaults["paletteDirList"])
	setSeatVar(&paletteFocus, seatDefaults["paletteFocus"])
	setSeatVar(&paletteSettings, seatDefaults["paletteSettings"])
	setSeatVar(&panesDelete, seatDefaults["panesDelete"])
	setSeatVar(&powerFocus, seatDefaults["powerFocus"])
	setSeatVar(&powerRows, seatDefaults["powerRows"])
	setSeatVar(&printTickFn, seatDefaults["printTickFn"])
	setSeatVar(&procsAlive, seatDefaults["procsAlive"])
	setSeatVar(&procsCpuTicks, seatDefaults["procsCpuTicks"])
	setSeatVar(&procsExitStatus, seatDefaults["procsExitStatus"])
	setSeatVar(&queryWindow, seatDefaults["queryWindow"])
	setSeatVar(&railCloseHover, seatDefaults["railCloseHover"])
	setSeatVar(&railDragFrom, seatDefaults["railDragFrom"])
	setSeatVar(&railHover, seatDefaults["railHover"])
	setSeatVar(&railMenuFocus, seatDefaults["railMenuFocus"])
	setSeatVar(&readAppsFile, seatDefaults["readAppsFile"])
	setSeatVar(&readNotifyFile, seatDefaults["readNotifyFile"])
	setSeatVar(&readSessionFile, seatDefaults["readSessionFile"])
	setSeatVar(&readWindowName, seatDefaults["readWindowName"])
	setSeatVar(&restoreSettingsWM, seatDefaults["restoreSettingsWM"])
	setSeatVar(&saveNotifyDND, seatDefaults["saveNotifyDND"])
	setSeatVar(&seatA11yDelete, seatDefaults["seatA11yDelete"])
	setSeatVar(&seatA11yWrite, seatDefaults["seatA11yWrite"])
	setSeatVar(&secretAskCommit, seatDefaults["secretAskCommit"])
	setSeatVar(&secretAskDrop, seatDefaults["secretAskDrop"])
	setSeatVar(&secretAskList, seatDefaults["secretAskList"])
	setSeatVar(&secretLog, seatDefaults["secretLog"])
	setSeatVar(&selFileDelete, seatDefaults["selFileDelete"])
	setSeatVar(&selFileRead, seatDefaults["selFileRead"])
	setSeatVar(&selFileWrite, seatDefaults["selFileWrite"])
	setSeatVar(&selLog, seatDefaults["selLog"])
	setSeatVar(&selPost, seatDefaults["selPost"])
	setSeatVar(&sendProgressNotice, seatDefaults["sendProgressNotice"])
	setSeatVar(&sendSettingsNotice, seatDefaults["sendSettingsNotice"])
	setSeatVar(&sessionSeq, seatDefaults["sessionSeq"])
	setSeatVar(&setCursorShape, seatDefaults["setCursorShape"])
	setSeatVar(&setWindowRect, seatDefaults["setWindowRect"])
	setSeatVar(&shotList, seatDefaults["shotList"])
	setSeatVar(&shotLog, seatDefaults["shotLog"])
	setSeatVar(&shotMkdir, seatDefaults["shotMkdir"])
	setSeatVar(&shotNow, seatDefaults["shotNow"])
	setSeatVar(&shotRead, seatDefaults["shotRead"])
	setSeatVar(&shotWrite, seatDefaults["shotWrite"])
	setSeatVar(&snapDelete, seatDefaults["snapDelete"])
	setSeatVar(&snapExists, seatDefaults["snapExists"])
	setSeatVar(&snapList, seatDefaults["snapList"])
	setSeatVar(&snapLog, seatDefaults["snapLog"])
	setSeatVar(&snapMkdir, seatDefaults["snapMkdir"])
	setSeatVar(&snapRead, seatDefaults["snapRead"])
	setSeatVar(&snapWrite, seatDefaults["snapWrite"])
	setSeatVar(&spaceN, seatDefaults["spaceN"])
	setSeatVar(&spaces, seatDefaults["spaces"])
	setSeatVar(&speechTickFn, seatDefaults["speechTickFn"])
	setSeatVar(&stateFileDelete, seatDefaults["stateFileDelete"])
	setSeatVar(&stateFileRead, seatDefaults["stateFileRead"])
	setSeatVar(&stateFileWrite, seatDefaults["stateFileWrite"])
	setSeatVar(&stateLog, seatDefaults["stateLog"])
	setSeatVar(&stateNoticePost, seatDefaults["stateNoticePost"])
	setSeatVar(&stateServiceDrain, seatDefaults["stateServiceDrain"])
	setSeatVar(&stateYield, seatDefaults["stateYield"])
	setSeatVar(&summonFocus, seatDefaults["summonFocus"])
	setSeatVar(&surfaceAttach, seatDefaults["surfaceAttach"])
	setSeatVar(&surfaceName, seatDefaults["surfaceName"])
	setSeatVar(&vsockCtlRequest, seatDefaults["vsockCtlRequest"])
	setSeatVar(&wdKill, seatDefaults["wdKill"])
	setSeatVar(&wdLog, seatDefaults["wdLog"])
	setSeatVar(&wdPrincipal, seatDefaults["wdPrincipal"])
	setSeatVar(&wdProcsUsage, seatDefaults["wdProcsUsage"])
	setSeatVar(&wmctlSetRect, seatDefaults["wmctlSetRect"])
	setSeatVar(&wmctlSetVisible, seatDefaults["wmctlSetVisible"])
	setSeatVar(&wqSend, seatDefaults["wqSend"])
	setSeatVar(&writeHostFile, seatDefaults["writeHostFile"])
	setSeatVar(&writeNotifyFile, seatDefaults["writeNotifyFile"])
	setSeatVar(&writeWindowChrome, seatDefaults["writeWindowChrome"])
	agentMilestones = slices.Clone(defAgentMilestones)
	seatChords = maps.Clone(defSeatChords)
	// theme is another package's mutable state (Current/baseName/
	// contrastHigh); the seat's boot state is dark with contrast off.
	theme.Set("dark")
	theme.SetContrast(false)
}

// resetSeatForTest returns the seat to its boot state and registers the
// same reset as cleanup, so every gotabwm test starts — and leaves —
// from the same zeroed seat.
func resetSeatForTest(t *testing.T) {
	t.Helper()
	captureSeatDefaults()
	applySeatReset()
	t.Cleanup(applySeatReset)
}
