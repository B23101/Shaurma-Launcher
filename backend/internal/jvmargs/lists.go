package jvmargs

// Єдине місце для правок списків. Усі порівняння — без урахування регістру
// (рядки тут у нижньому регістрі).

// blockedPrefixes — рівень A: класи аргументів, які виконують код, вантажать
// довільні файли або перенаправляють завантаження/логи. Співпадіння за
// ПРЕФІКСОМ (тож «-javaagent» ловить і «-javaagent:x.jar»).
var blockedPrefixes = []string{
	// агенти й хуки виконання коду
	"-javaagent", "-agentlib", "-agentpath", "-xrun", "-xbootclasspath",
	"-xx:onerror", "-xx:onoutofmemoryerror", "-xx:abortvmonexception",
	"-xx:vmoptionsfile", "-xx:flightrecorderoptions", "-xx:startflightrecording",
	"-xx:flags=", "-xx:compilecommandfile", "-xx:replaydatafile",
	// шляхи завантаження класів/модулів
	"--class-path", "--module-path", "--upgrade-module-path", "--patch-module",
	"--module=",
	// файли на диску, які пише/читає JVM
	"-xx:heapdumppath", "-xx:errorfile", "-xx:logfile", "-xlog", "-xloggc",
	"-xshare:", "-xx:sharedarchivefile",
	// перенаправлення завантаження та конфігурації
	"-djava.library.path", "-djava.class.path", "-djava.ext.dirs",
	"-djava.endorsed.dirs", "-dsun.boot", "-djdk.module",
	"-dlog4j.configurationfile", "-dlog4j.configuration", "-dlog4j2.configurationfile",
	"-dfabric.addmods", "-dfabric.loadmods",
	"-djava.system.class.loader", "-djava.security.manager",
	"-djava.security.policy", "-djava.security.properties",
	"-dorg.lwjgl.librarypath",
}

// blockedExact — точні збіги (короткі прапорці, префікс яких зловив би зайве).
var blockedExact = map[string]bool{
	"-cp": true, "-classpath": true, "-p": true, "-m": true,
	"--module": true, "-jar": true,
}

// allowedBoolFlags — рівень B: -XX:+Name / -XX:-Name.
var allowedBoolFlags = map[string]bool{}

func init() {
	for _, n := range []string{
		"UseG1GC", "UseZGC", "ZGenerational", "UseShenandoahGC", "UseParallelGC",
		"UseSerialGC", "UseConcMarkSweepGC", "ParallelRefProcEnabled",
		"DisableExplicitGC", "AlwaysPreTouch", "UseStringDeduplication",
		"OptimizeStringConcat", "UseFastUnorderedTimeStamps", "UseNUMA",
		"UseLargePages", "UseTransparentHugePages", "PerfDisableSharedMem",
		"UseCompressedOops", "UseCompressedClassPointers", "TieredCompilation",
		"UseAdaptiveSizePolicy", "UseCodeCacheFlushing", "AggressiveOpts",
		"UnlockExperimentalVMOptions", "IgnoreUnrecognizedVMOptions",
		"ShowCodeDetailsInExceptionMessages", "OmitStackTraceInFastThrow",
		"UseContainerSupport", "UseStringCache", "UseBiasedLocking",
		"G1UseAdaptiveIHOP", "UseDynamicNumberOfGCThreads", "ClassUnloading",
		"UseThreadPriorities", "UseCompressedOops", "AlwaysActAsServerClassMachine",
	} {
		allowedBoolFlags[lower(n)] = true
	}
	for _, n := range []string{
		"MaxGCPauseMillis", "G1HeapRegionSize", "InitiatingHeapOccupancyPercent",
		"G1NewSizePercent", "G1MaxNewSizePercent", "G1ReservePercent",
		"G1HeapWastePercent", "G1MixedGCCountTarget", "G1MixedGCLiveThresholdPercent",
		"G1RSetUpdatingPauseTimePercent", "G1SATBBufferEnqueueingThresholdPercent",
		"SurvivorRatio", "MaxTenuringThreshold", "TargetSurvivorRatio",
		"ParallelGCThreads", "ConcGCThreads", "GCTimeRatio", "MaxMetaspaceSize",
		"MetaspaceSize", "ReservedCodeCacheSize", "SoftRefLRUPolicyMSPerMB",
		"ThreadStackSize", "CICompilerCount", "MaxRAMPercentage",
		"MinRAMPercentage", "InitialRAMPercentage", "MaxDirectMemorySize",
		"NewSize", "MaxNewSize", "ZCollectionInterval", "ZUncommitDelay",
		"UseAVX", "UseSSE", "MaxInlineLevel", "FreqInlineSize",
	} {
		allowedValueFlags[lower(n)] = true
	}
}

// allowedValueFlags — рівень B: -XX:Name=value (значення: число або розмір).
var allowedValueFlags = map[string]bool{}

// allowedProps — рівень B: -Dname=value. Значення за замовчуванням
// [A-Za-z0-9._-]{0,64}; для винятків є окремі шаблони в propValueRelaxed.
var allowedProps = map[string]bool{
	"file.encoding": true, "sun.stdout.encoding": true, "sun.stderr.encoding": true,
	"stdout.encoding": true, "stderr.encoding": true, "native.encoding": true,
	"user.language": true, "user.country": true, "user.timezone": true,
	"fml.readtimeout": true, "fml.ignoreinvalidminecraftcertificates": true,
	"fml.ignorepatchdiscrepancies": true, "log4j2.formatmsgnolookups": true,
	"java.net.preferipv4stack": true, "java.awt.headless": true,
	"sun.java2d.opengl": true, "sun.java2d.d3d": true, "sun.java2d.noddraw": true,
	"org.lwjgl.util.debug": true, "org.lwjgl.util.debugloader": true,
	"using.aikars.flags": true, "aikars.new.flags": true,
}

// propValueRelaxed — властивості, значення яких законно містить «:» чи «/».
var propValueRelaxed = map[string]bool{
	"using.aikars.flags": true,
}
