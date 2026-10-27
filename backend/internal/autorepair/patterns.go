package autorepair

import "regexp"

// Назви та порядок видів проблем (порядок — порядок показу).
type issueKind struct{ ID, Title string }

var issueKinds = []issueKind{
	{"account", "Проблема з акаунтом"},
	{"loader_broken", "Модлоадер пошкоджено або не запускається"},
	{"loader_outdated", "Застаріла версія модлоадера"},
	{"missing_dependencies", "Бракує залежностей модів"},
	{"incompatible_mods", "Несумісні моди"},
	{"wrong_loader_mods", "Моди для іншого завантажувача"},
	{"duplicate_mods", "Дублікати модів"},
	{"mixin_failure", "Помилка Mixin у моді"},
	{"corrupted_mods", "Пошкоджені файли модів"},
	{"broken_config", "Пошкоджені конфіги модів"},
	{"out_of_memory", "Не вистачило пам'яті"},
	{"memory_too_high", "Виділено забагато пам'яті"},
	{"java_version", "Невідповідна версія Java"},
	{"jvm_arguments", "Помилкові аргументи JVM"},
	{"jvm_crash", "Аварійне падіння Java"},
	{"game_files", "Пошкоджено файли гри чи завантажувача"},
	{"graphics", "Проблема з графікою"},
	{"recent_mods", "Нещодавно додані моди"},
	{"unknown_crash", "Невідома причина краху"},
}

// IssueKind — публічний опис виду проблеми (для налаштувань «приховати вид»).
type IssueKind struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// IssueKinds повертає усі види проблем у порядку показу.
func IssueKinds() []IssueKind {
	out := make([]IssueKind, 0, len(issueKinds))
	for _, k := range issueKinds {
		out = append(out, IssueKind{ID: k.ID, Title: k.Title})
	}
	return out
}

func kindTitle(id string) string {
	for _, k := range issueKinds {
		if k.ID == id {
			return k.Title
		}
	}
	return id
}

func re(p string) *regexp.Regexp { return regexp.MustCompile(`(?i)` + p) }

// Залежність у повідомленні Fabric: 'Name' (id) | 'id' | id
const depPattern = `(?:'([^']+)' \(([\w.\-]+)\)|'([\w.\-]+)'|([\w.\-]+))`

var (
	fabricRequires     = re(`Mod '([^']+)' \(([\w.\-]+)\)[^\n]*? requires ([^\n]*?) of (?:mod )?` + depPattern + `,\s*which is missing`)
	fabricWrongVersion = re(`Mod '([^']+)' \(([\w.\-]+)\)[^\n]*? requires ([^\n]*?) of (?:mod )?` + depPattern + `,\s*but only the wrong version is present`)
	fabricIncompatible = re(`Mod '([^']+)' \(([\w.\-]+)\)[^\n]*?is incompatible with [^\n]*?(?:mod )?'([^']+)'(?: \(([\w.\-]+)\))?`)
	forgeDependency    = re(`Mod ID: '([\w.\-]+)', Requested by: '([\w.\-]+)', Expected range: '([^']*)', Actual version: '([^']*)'`)
	forgeDuplicate     = re(`Mod ID: '([\w.\-]+)' from mod files: ([^\n]+)`)
	mixinForMod        = re(`Mixin apply (?:for|of) mod ([\w.\-]+) failed`)
	mixinFromMod       = re(`from mod ([\w.\-]+)(?:\]| failed| ->)`)
	mixinConfig        = re(`([\w.\-]+\.mixins?\.json)`)
	moduleConflict     = re(`Modules? ([\w.\-]+) and ([\w.\-]+) export package`)
	moduleReads        = re(`reads package [\w.]+ from both ([\w.\-]+) and ([\w.\-]+)`)
	crashModFile       = re(`Mod File: (?:[^\n]*[/\\])?([^\n/\\]+\.jar)`)
	crashSuspectID     = re(`(?m)^\s+[^\n(]+\(([\w.\-]+)\),\s*Version`)
	classFileVersion   = re(`class file version (\d+)(?:\.\d+)?`)
	requiresJava       = re(`(?:requires (?:version )?(\d{1,2})(?: or later)? of (?:mod )?'?java'?|requires java (\d{1,2})|потрібна Java (\d{1,2})|Java (\d{1,2}) or (?:newer|later) is required)`)
	configPath         = re(`config[/\\]([\w.\-/\\ ]+?\.(?:toml|json5?|cfg|properties|ya?ml|txt|snbt))`)
	forgeConfigFile    = re(`(?:Failed loading config file|Failed to load config|Error loading config|Configuration file) ([\w.\-]+\.(?:toml|json5?|cfg))`)

	oom           = re(`java\.lang\.OutOfMemoryError|GC overhead limit exceeded|Java heap space`)
	memoryTooHigh = re(`Could not reserve enough space for|Invalid maximum heap size|insufficient memory for the Java Runtime|Native memory allocation \(mmap\) failed|Cannot allocate memory`)
	jvmArguments  = re(`Unrecognized VM option|Unrecognized option:|Error occurred during initialization of VM|Invalid initial heap size|Conflicting collector combinations|Multiple garbage collectors selected|Improperly specified VM option|Could not create the Java Virtual Machine`)
	jvmCrash      = re(`A fatal error has been detected by the Java Runtime|SIGSEGV|SIGBUS|EXCEPTION_ACCESS_VIOLATION|hs_err_pid\d+`)
	driverFrame   = re(`atio6axx|atioglxx|nvoglv|ig\d+icd|libnvidia|amdvlk|radeonsi|iris_dri|i965_dri|libGLX|opengl32`)
	// Графіка: класичні GLFW/OpenGL-помилки + новий рендер-стек MC 26.x.
	// У 26.x гра пробує OpenGL і Vulkan, і якщо жоден бекенд не піднявся,
	// LWJGL аварійно завершує JVM у native-коді — без Java-винятку й без
	// звіту про крах. Без цих підписів такий крах потрапляв у «Невідома
	// причина», і користувач не бачив підказки про драйвер відеокарти.
	graphics = re(`no OpenGL context|could not create (?:the )?(?:GL|OpenGL)|GLFW error (?:65542|65543|65544|65545)|failed to create window|Pixel format not accelerated|WGL: The driver does not appear to support OpenGL|OpenGL[^\n]{0,40}not supported|GLX: Failed to create context|Failed to create OpenGL context` +
		`|Could not find any graphics adapters|No supported graphics backend was found|Failed to create backend (?:OpenGL|Vulkan)|WindowInitFailed|No context is current|FATAL ERROR in native method: \[LWJGL\]`)
	// graphicsNoAdapter — підпис «ані OpenGL, ані Vulkan не знайшли жодного
	// адаптера». Майже завжди це драйвер відеокарти (або гру примушено на
	// недоступну GPU), тому підказка для користувача інша, ніж у решти
	// графічних збоїв.
	graphicsNoAdapter = re(`Could not find any graphics adapters|No supported graphics backend was found|Failed to create backend (?:OpenGL|Vulkan)|WindowInitFailed|No context is current|FATAL ERROR in native method: \[LWJGL\]`)

	gameFiles = re(`Could not find or load main class` +
		`|ClassNotFoundException: (?:net\.fabricmc\.loader|cpw\.mods|net\.minecraftforge\.(?:fml|bootstrap)|net\.neoforged\.(?:fml|bootstrap)|org\.quiltmc\.loader|net\.minecraft\.client\.main|net\.minecraft\.launchwrapper)` +
		`|NoClassDefFoundError: (?:org/lwjgl|net/fabricmc/loader|cpw/mods|net/minecraftforge/fml/loading|net/neoforged/fml)` +
		`|no lwjgl[\w]* in java\.library\.path|UnsatisfiedLinkError|Failed to locate library|checksum mismatch|SHA-?1 mismatch|hash mismatch|Failed to download` +
		`|(?:FileNotFound|NoSuchFile)Exception[^\n]*(?:libraries|versions|assets)[/\\]` +
		`|zip END header not found[^\n]*(?:libraries|versions)` +
		`|не знайдено клієнтський jar Minecraft|processor jar відсутній|classpath-бібліотека processor|не вдалося завантажити після \d+ спроб|файл не знайдено \(404\)`)
	loaderBroken = re(`Could not find or load main class (?:net\.fabricmc|org\.quiltmc|cpw\.mods|net\.minecraftforge|net\.neoforged)` +
		`|ClassNotFoundException: (?:net\.fabricmc\.loader|org\.quiltmc\.loader|cpw\.mods|net\.minecraftforge\.(?:fml|bootstrap)|net\.neoforged\.(?:fml|bootstrap|neoforgespi))` +
		`|NoClassDefFoundError: (?:net/fabricmc/loader|org/quiltmc/loader|cpw/mods|net/minecraftforge/fml|net/neoforged/fml)` +
		`|Failed to find system mod|Missing (?:minecraft|forge|neoforge|mcp) (?:jar|installation)|InvalidLauncherSetupException` +
		`|Failed to find (?:the )?Minecraft (?:client )?jar|Unable to locate (?:the )?minecraft|Could not find required file` +
		`|client-[\w.]+-(?:srg|extra|slim|official)\.jar|java\.lang\.module\.FindException` +
		`|There is no minecraft launcher profile|There was an error during installation|A problem installing was detected` +
		`|net\.fabricmc\.loader\.impl\.FormattedException: (?:java\.lang\.RuntimeException: )?(?:Failed to|Could not)` +
		`|Loader version [^\n]{0,40}is not compatible|Failed to load (?:FML|ModLauncher)` +
		`|не вдалося створити jar профілю лоадера|встановлення Forge неможливо продовжити`)
	// NeoForge (FML 4+): «Mod create requires neoforge 21.1.77 or above\nCurrently, neoforge is 21.1.50».
	fmlRequires = re(`Mod ([\w.\-]+) requires ([\w.\-]+) ([\w.\-+]+) or above\s*(?:\n\s*)?Currently, ([\w.\-]+) is (not installed|[\w.\-+]+)`)

	corruptedZip = re(`zip END header not found|invalid LOC header|error in opening zip file|ZipException|Invalid or corrupt jarfile|BadZipFile|пошкоджений архів`)
	javaMissing  = re(`Java не знайдено|Cannot run program "[^"]*java`)

	incompatibleSet = re(`some of your mods are incompatible|incompatible mod set|Mod resolution failed|ModResolutionException`)
	mixinKind       = re(`MixinApplyError|MixinTransformerError|InvalidInjectionException|InjectionError|Mixin apply|Critical injection failure|MixinPrepareError`)
	configParseErr  = re(`ParsingException|JsonSyntaxException|MalformedJsonException|Failed (?:loading|to load) config|Error loading config|Config(?:uration)? (?:file )?(?:is )?(?:corrupt|invalid|not correct)|TomlParsingException|ParseException`)
	nonLatinPath    = regexp.MustCompile(`[A-Za-z]:\\[^\n]*[^\x00-\x7f]`)
	errorWord       = re(`error|exception|caused by`)
	mixinWord       = re(`mixin`)
	heapSizeWord    = re(`heap size`)
	suspectBlock    = re(`(?s)Suspected Mods?:(.*?)(?:\n\S|\n\n|\z)`)
	parenID         = regexp.MustCompile(`\(([\w.\-]+)\)`)
	loaderFileName  = regexp.MustCompile(`(?i)^(?:minecraft|client|forge|neoforge|fabric-loader|quilt-loader)[-.]`)

	// ── Акаунти ──
	accountRelogin = re(`invalid_grant|AADSTS\d+|Microsoft OAuth|InvalidCredentialsException|Invalid token|Invalid session|Failed to verify username|Bad login|User not premium` +
		`|(?:sessionserver|authserver|api\.minecraftservices)[^\n]{0,120}\b401\b|токен[^\n]{0,40}(?:прострочен|закінчи|недійсн)|сесі[яю] [^\n]{0,30}(?:прострочен|застаріла)`)
	accountMissing   = re(`акаунт[^\n]{0,30}не знайдено|Account not found|account no longer available`)
	accountNoLicense = re(`does not own (?:Minecraft|the game)|NOT_FOUND[^\n]{0,60}profile|немає ліцензії Minecraft|не знайдено права на Minecraft`)
	accountXSTS      = re(`XSTS[^\n]{0,60}?(\d{10})|(акаунт не прив'язаний до Xbox Live)|(дитячий акаунт — потрібен дозвіл батьків)`)
	accountStorage   = re(`сховище паролів недоступне|libsecret|secret-tool[^\n]{0,80}(?:not found|failed)|CredWrite|CredRead`)
	accountMulti     = re(`Multiplayer is disabled|multiplayer (?:has been )?disabled|UserBannedException|banned from (?:online play|multiplayer)`)
)

// Підказки за кодами XSTS.
var xstsHints = map[string]string{
	"2148916233": "У цього акаунта Microsoft немає профілю Xbox. Увійдіть на xbox.com, створіть профіль і повторіть вхід.",
	"2148916235": "Xbox Live недоступний у країні акаунта. Змініть регіон акаунта Microsoft або використайте інший акаунт.",
	"2148916236": "Акаунту потрібна перевірка віку на xbox.com — пройдіть її й повторіть вхід.",
	"2148916237": "Акаунту потрібна перевірка віку на xbox.com — пройдіть її й повторіть вхід.",
	"2148916238": "Це дитячий акаунт: дорослий член сім'ї Microsoft має додати його до сім'ї й дозволити гру.",
}

// Ідентифікатори самих завантажувачів у маніфестах і повідомленнях про залежності.
var loaderDependencyIDs = map[string]map[string]bool{
	"fabric":   set("fabricloader", "fabric-loader", "fabric_loader"),
	"quilt":    set("quilt_loader", "quilt-loader", "quiltloader"),
	"forge":    set("forge", "fml", "javafml"),
	"neoforge": set("neoforge", "fml", "javafml"),
}

var allLoaderDependencyIDs = func() map[string]bool {
	out := map[string]bool{}
	for _, s := range loaderDependencyIDs {
		for k := range s {
			out[k] = true
		}
	}
	return out
}()

var loaderTitles = map[string]string{"fabric": "Fabric", "quilt": "Quilt", "forge": "Forge", "neoforge": "NeoForge", "vanilla": "Vanilla"}

var shaderModIDs = set("iris", "oculus", "optifabric", "optifine", "canvas", "irisshaders")
var libraryDependencyIDs = set("java", "minecraft")

func set(items ...string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, i := range items {
		out[i] = true
	}
	return out
}
