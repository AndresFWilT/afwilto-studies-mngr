package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/afwilto/studies-mngr/configs"
	examshttp "github.com/afwilto/studies-mngr/internal/exams/adapter/in/http"
	"github.com/afwilto/studies-mngr/internal/exams/adapter/out/fsstore"
	examspostgres "github.com/afwilto/studies-mngr/internal/exams/adapter/out/postgres"
	examssystem "github.com/afwilto/studies-mngr/internal/exams/adapter/out/system"
	examsusecase "github.com/afwilto/studies-mngr/internal/exams/app/usecase"
	"github.com/afwilto/studies-mngr/internal/platform/httpx"
	studieshttp "github.com/afwilto/studies-mngr/internal/studies/adapter/in/http"
	"github.com/afwilto/studies-mngr/internal/studies/adapter/out/postgres"
	"github.com/afwilto/studies-mngr/internal/studies/adapter/out/system"
	"github.com/afwilto/studies-mngr/internal/studies/app/usecase"
)

// main is the composition root: the only place that knows concrete adapter types.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := configs.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("creating database pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if cfg.MigrateOnStart {
		if err := runMigrations(cfg.DatabaseURL); err != nil {
			slog.Error("running migrations", "err", err)
			os.Exit(1)
		}
	}

	clock := system.Clock{}
	ids := system.UUIDv7Generator{}
	categories := postgres.NewCategoryRepository(pool)
	noteKinds := postgres.NewNoteKindRepository(pool)
	subjects := postgres.NewSubjectRepository(pool)
	notes := postgres.NewNoteRepository(pool)
	tags := postgres.NewTagQuery(pool)
	subjectExams := postgres.NewSubjectExamCounter(pool)

	// The media directory is a volume in the platform; if it cannot be made the
	// service still starts and /readyz says why it is not ready.
	if err := os.MkdirAll(cfg.MediaDir, 0o755); err != nil {
		slog.Error("creating the media directory", "dir", cfg.MediaDir, "err", err)
	}
	mediaStore := fsstore.NewDiskFileStorage(cfg.MediaDir)

	studiesDeps := studieshttp.RouterDeps{
		Pinger: newReadiness(pool, mediaStore),

		ListCategories: usecase.NewListCategoriesService(categories),
		CreateCategory: usecase.NewCreateCategoryService(categories, clock, ids),
		GetCategory:    usecase.NewGetCategoryService(categories),
		UpdateCategory: usecase.NewUpdateCategoryService(categories, clock),
		DeleteCategory: usecase.NewDeleteCategoryService(categories, subjects, clock),

		ListNotesByTag:   usecase.NewListNotesByTagService(notes),
		ListTags:         usecase.NewListTagsService(tags),
		ListSubjectNotes: usecase.NewListSubjectNotesService(notes, subjects),
		CreateNote:       usecase.NewCreateNoteService(notes, subjects, noteKinds, clock, ids),
		GetNote:          usecase.NewGetNoteService(notes),
		UpdateNote:       usecase.NewUpdateNoteService(notes, subjects, noteKinds, clock),
		DeleteNote:       usecase.NewDeleteNoteService(notes, clock),

		ListSubjects:  usecase.NewListSubjectsService(subjects),
		CreateSubject: usecase.NewCreateSubjectService(subjects, categories, clock, ids),
		GetSubject:    usecase.NewGetSubjectService(subjects),
		UpdateSubject: usecase.NewUpdateSubjectService(subjects, categories, clock),
		DeleteSubject: usecase.NewDeleteSubjectService(subjects, notes, subjectExams, clock),

		ListNoteKinds:  usecase.NewListNoteKindsService(noteKinds),
		CreateNoteKind: usecase.NewCreateNoteKindService(noteKinds, clock, ids),
		GetNoteKind:    usecase.NewGetNoteKindService(noteKinds),
		RenameNoteKind: usecase.NewRenameNoteKindService(noteKinds, clock),
		DeleteNoteKind: usecase.NewDeleteNoteKindService(noteKinds, notes, clock),
	}

	examRepo := examspostgres.NewExamRepository(pool)
	examSubjects := examspostgres.NewSubjectDirectory(pool)
	questionBank := examspostgres.NewQuestionBankRepository(pool)
	mediaIndex := examspostgres.NewMediaRepository(pool)
	attemptRepo := examspostgres.NewAttemptRepository(pool)
	questionFiles, err := examshttp.NewQuestionFileReader() // compiles the embedded schema once
	if err != nil {
		slog.Error("compiling the question import schema", "err", err)
		os.Exit(1)
	}
	examDeps := examshttp.Deps{
		CreateExam:       examsusecase.NewCreateExamService(examRepo, examSubjects, clock, ids),
		ListSubjectExams: examsusecase.NewListSubjectExamsService(examRepo, examSubjects),
		GetExam:          examsusecase.NewGetExamService(examRepo),
		UpdateExam:       examsusecase.NewUpdateExamService(examRepo, questionBank, clock),
		DeleteExam:       examsusecase.NewDeleteExamService(examRepo, clock),
		ImportQuestions:  examsusecase.NewImportQuestionsService(questionBank, clock, ids, examsusecase.WithMedia(mediaIndex, mediaStore)),
		FileReader:       questionFiles,

		ListExamQuestions: examsusecase.NewListExamQuestionsService(questionBank),
		GetQuestion:       examsusecase.NewGetQuestionService(questionBank),
		DeleteQuestion:    examsusecase.NewDeleteQuestionService(questionBank, clock),
		MergeCategories:   examsusecase.NewMergeCategoriesService(examRepo, questionBank, clock),

		GetMedia:        examsusecase.NewGetMediaService(mediaIndex, mediaStore),
		ExportQuestions: examsusecase.NewExportQuestionsService(questionBank, mediaIndex),

		PreviewSession: examsusecase.NewPreviewSessionService(examRepo, questionBank),
		StartAttempt:   examsusecase.NewStartAttemptService(examRepo, questionBank, attemptRepo, clock, ids, examssystem.RandomSeeds{}),

		GetAttempt:       examsusecase.NewGetAttemptService(attemptRepo, clock),
		AnswerItem:       examsusecase.NewAnswerItemService(attemptRepo, clock),
		SubmitAttempt:    examsusecase.NewSubmitAttemptService(attemptRepo, clock),
		ReviewAttempt:    examsusecase.NewReviewAttemptService(attemptRepo, examspostgres.NewNoteDirectory(pool), clock),
		ListExamAttempts: examsusecase.NewListExamAttemptsService(examRepo, attemptRepo),
	}

	router := httpx.NewServer(httpx.Config{
		AllowOrigins: cfg.AllowOrigins,
		MaxBodyBytes: cfg.MaxBodyBytes,
		BodyLimits:   examshttp.BodyLimits(cfg.MaxImportBytes),
	}, studieshttp.Register(studiesDeps), examshttp.Register(examDeps))

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("studies-mngr starting", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server terminated", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	}
}
